package main

import (
	"flag"
	"fmt"
	template "html/template"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/h2non/filetype"
	log "github.com/schollz/logger"
)

type stream struct {
	b    []byte
	done bool
}

type dataStruct struct {
	Title    string
	Items    []string
	Rand     string
	Archived []ArchivedFile
}

var flagDebug bool
var flagPort int
var flagFolder string
var mutex *sync.Mutex
var tplmain *template.Template
var channels map[string]map[float64]chan stream
var archived map[string]*os.File
var advertisements map[string]bool

func init() {
	flag.StringVar(&flagFolder, "folder", "archived", "folder to save archived")
	flag.IntVar(&flagPort, "port", 9222, "port for server")
	flag.BoolVar(&flagDebug, "debug", false, "debug mode")
}

func main() {
	flag.Parse()
	os.MkdirAll(flagFolder, os.ModePerm)
	// use all of the processors
	runtime.GOMAXPROCS(runtime.NumCPU())
	if flagDebug {
		log.SetLevel("debug")
		log.Debug("debug mode")
	} else {
		log.SetLevel("info")
	}
	if err := serve(); err != nil {
		panic(err)
	}
}

func handleMainPage(w http.ResponseWriter, r *http.Request) {
	adverts := []string{}
	mutex.Lock()
	for advert := range advertisements {
		adverts = append(adverts, strings.TrimPrefix(advert, "/"))
	}
	mutex.Unlock()

	active := make(map[string]struct{})

	data := dataStruct{
		Title:    "Current broadcasts",
		Items:    adverts,
		Rand:     fmt.Sprintf("%d", rand.Int31()),
		Archived: listArchived(active),
	}
	err := tplmain.Execute(w, data)
	if err != nil {
		log.Errorf("Err: failure serving main page: [err=%s]", err)
	}
}

func handleArchivedPage(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"+flagFolder+"/"))
	// this extra join implicitly does a clean and thereby prevents directory traversal
	filename = path.Join("/", filename)
	filename = path.Join(flagFolder, filename)
	v, ok := r.URL.Query()["remove"]
	if ok && v[0] == "true" {
		os.Remove(filename)
		w.Write([]byte(fmt.Sprintf("removed %s", filename)))
	} else {
		v, ok := r.URL.Query()["rename"]
		if ok && v[0] == "true" {
			newname_param, ok := r.URL.Query()["newname"]
			if !ok {
				w.Write([]byte(fmt.Sprintf("ERROR")))
				return
			}
			// this join with "/" prevents directory traversal with an implicit clean
			newname := newname_param[0]
			newname = path.Join("/", newname)
			newname = path.Join(flagFolder, newname)
			os.Rename(filename, newname)
			w.Write([]byte(fmt.Sprintf("renamed %s to %s", filename, newname)))
		} else {
			http.ServeFile(w, r, filename)
		}
	}
}

func handleGetRequest(w http.ResponseWriter, r *http.Request) {
	id := rand.Float64()
	mutex.Lock()
	channels[r.URL.Path][id] = make(chan stream, 30)
	channel := channels[r.URL.Path][id]
	log.Debugf("added listener %f", id)
	mutex.Unlock()

	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Cache-Control", "no-cache, no-store")

	mimetyped := false
	canceled := false
	for {
		select {
		case s := <-channel:
			if s.done {
				canceled = true
			} else {
				if !mimetyped {
					mimetyped = true
					mimetype := mimetype.Detect(s.b).String()
					if mimetype == "application/octet-stream" {
						ext := strings.TrimPrefix(filepath.Ext(r.URL.Path), ".")
						log.Debug("checking extension %s", ext)
						mimetype = filetype.GetType(ext).MIME.Value
					}
					w.Header().Set("Content-Type", mimetype)
					log.Debugf("serving as Content-Type: '%s'", mimetype)
				}
				w.Write(s.b)
				w.(http.Flusher).Flush()
			}
		case <-r.Context().Done():
			log.Debug("consumer canceled")
			canceled = true
		}
		if canceled {
			break
		}
	}

	mutex.Lock()
	delete(channels[r.URL.Path], id)
	log.Debugf("removed listener %f", id)
	mutex.Unlock()
	close(channel)
}

func handlePostRequest(w http.ResponseWriter, r *http.Request, doStream bool, doArchive bool) {
	buffer := make([]byte, 2048)
	cancel := true
	isdone := false
	lifetime := 0
	for {
		if !doStream {
			select {
			case <-r.Context().Done():
				isdone = true
			default:
			}
			if isdone {
				log.Debug("is done")
				break
			}
			mutex.Lock()
			numListeners := len(channels[r.URL.Path])
			mutex.Unlock()
			if numListeners == 0 {
				time.Sleep(1 * time.Second)
				lifetime++
				if lifetime > 600 {
					isdone = true
				}
				continue
			}
		}
		n, err := r.Body.Read(buffer)
		if err != nil {
			log.Errorf("err: %s", err)
			if err == io.ErrUnexpectedEOF {
				cancel = false
			}
			break
		}
		if doArchive {
			mutex.Lock()
			archived[r.URL.Path].Write(buffer[:n])
			mutex.Unlock()
		}
		mutex.Lock()
		channels_current := channels[r.URL.Path]
		mutex.Unlock()
		for _, c := range channels_current {
			var b2 = make([]byte, n)
			copy(b2, buffer[:n])
			c <- stream{b: b2}
		}
	}
	if cancel {
		mutex.Lock()
		channels_current := channels[r.URL.Path]
		mutex.Unlock()
		for _, c := range channels_current {
			c <- stream{done: true}
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	var err error

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

	log.Debugf("opened %s %s", r.Method, r.URL.Path)
	defer func() {
		log.Debugf("finished %s\n", r.URL.Path)
	}()

	if r.URL.Path == "/" {
		handleMainPage(w, r)
		return
	} else if r.URL.Path == "/favicon.ico" {
		w.WriteHeader(http.StatusOK)
		return
	} else if strings.HasPrefix(r.URL.Path, "/"+flagFolder+"/") {
		handleArchivedPage(w, r)
		return
	}

	v, ok := r.URL.Query()["stream"]
	doStream := ok && v[0] == "true"

	v, ok = r.URL.Query()["archive"]
	doArchive := ok && v[0] == "true"

	v, ok = r.URL.Query()["advertise"]
	doAdvertise := ok && v[0] == "true"

	if doArchive && r.Method == "POST" {
		if _, ok := archived[r.URL.Path]; !ok {
			folderName := path.Join(flagFolder, time.Now().Format("200601021504"))
			os.MkdirAll(folderName, os.ModePerm)
			archived[r.URL.Path], err = os.Create(path.Join(folderName, strings.TrimPrefix(r.URL.Path, "/")))
			if err != nil {
				log.Error(err)
			}
		}
		defer func() {
			mutex.Lock()
			if _, ok := archived[r.URL.Path]; ok {
				log.Debugf("closed archive for %s", r.URL.Path)
				archived[r.URL.Path].Close()
				delete(archived, r.URL.Path)
			}
			mutex.Unlock()
		}()
	}

	if doAdvertise && doStream {
		mutex.Lock()
		advertisements[r.URL.Path] = true
		mutex.Unlock()
		defer func() {
			mutex.Lock()
			delete(advertisements, r.URL.Path)
			mutex.Unlock()
		}()
	}

	mutex.Lock()
	if _, ok := channels[r.URL.Path]; !ok {
		channels[r.URL.Path] = make(map[float64]chan stream)
	}
	mutex.Unlock()

	if r.Method == "GET" {
		handleGetRequest(w, r)
	} else if r.Method == "POST" {
		handlePostRequest(w, r, doStream, doArchive)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}

// Serve will start the server
func serve() (err error) {
	channels = make(map[string]map[float64]chan stream)
	archived = make(map[string]*os.File)
	advertisements = make(map[string]bool)
	mutex = &sync.Mutex{}
	tplBin, err := os.ReadFile("mainpage.html.tpl")
	if err != nil {
		log.Errorf("Failed to open html template file: %s", err)
		return
	}
	tpl := string(tplBin)
	tplmain, err = template.New("webpage").Parse(tpl)
	if err != nil {
		return
	}

	log.Infof("running on port %d", flagPort)
	err = http.ListenAndServe(fmt.Sprintf(":%d", flagPort), http.HandlerFunc(handler))
	if err != nil {
		log.Error(err)
	}
	return
}

type ArchivedFile struct {
	Filename     string
	FullFilename string
	Created      time.Time
}

func listArchived(active map[string]struct{}) (afiles []ArchivedFile) {
	fnames := []string{}
	err := filepath.Walk(flagFolder,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				fnames = append(fnames, path)
			}
			return nil
		})
	if err != nil {
		return
	}
	for _, fname := range fnames {
		_, onlyfname := path.Split(fname)
		finfo, _ := os.Stat(fname)
		stat_t := finfo.Sys().(*syscall.Stat_t)
		created := timespecToTime(stat_t.Ctim)
		if _, ok := active[onlyfname]; !ok {
			afiles = append(afiles, ArchivedFile{
				Filename:     onlyfname,
				FullFilename: fname,
				Created:      created,
			})
		}
	}

	sort.Slice(afiles, func(i, j int) bool {
		return afiles[i].Created.After(afiles[j].Created)
	})

	return
}

func timespecToTime(ts syscall.Timespec) time.Time {
	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
}
