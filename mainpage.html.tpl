<!DOCTYPE html>
<html>
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>{{.Title}}</title>
    <style type="text/css">
      body {
        margin: 40px auto;
        max-width: 650px;
        line-height: 1.6;
        font-size: 18px;
        padding: 0 10px;
      }
      h1,
      h2,
      h3 {
        line-height: 1.2;
      }
      .delete {
        color: #000;
        text-decoration: none;
      }

      summary {
        white-space: nowrap;
        cursor: pointer;
      }

      details > summary {
        list-style: none;
      }
      details > summary::-webkit-details-marker {
        display: none;
      }

      details {
        display: inline;
      }

      details[open] {
        padding: 1em;
        display: block;
      }
    </style>
  </head>
  <body>
    <h1>Current broadcasts:</h1>
    {{range .Items}}<a href="/{{ . }}">{{ . }}</a><br />
    <audio controls preload="none">
      <source src="/{{ . }}?r={{$.Rand}}" type="audio/mpeg" />
      Your browser does not support the audio element.
    </audio>
    <br /><br />
    {{else}}
    <div><strong>No broadcasts currently.</strong></div>
    {{end}}
    <h2>Archived broadcasts:</h2>
    {{if .Archived}}
    <p>
      <small
        >click ❌ to remove an archive, ✎ to rename an archive (<em
          >maybe don't remove/rename ones that you didn't create</em
        >).</small
      >
    </p>
    {{end}} {{range .Archived}}<a href="/{{ .FullFilename }}"
      >{{ .Filename }}</a
    >
    <small
      >({{.Created.Format "Jan 02, 2006 15:04:05 UTC"}},
      <details>
        <summary>❌</summary>
        are you sure?
        <details>
          <summary>click->👍</summary>
          absolutely sure?
          <a class="delete" href="/{{ .FullFilename }}?remove=true">🗑️</a>
        </details>
      </details>
      <details>
        <summary>✎</summary>
        <form method="get" action="/{{ .FullFilename }}">
          <input type="hidden" name="rename" value="true" />
          Rename to:
          <input type="text" name="newname" value="{{ .Filename }}" />
          <input type="submit" />
        </form>
      </details>
      ) </small
    ><br />
    <audio controls preload="none">
      <source src="/{{ .FullFilename }}?r={{$.Rand}}" type="audio/mpeg" />
      Your browser does not support the audio element.</audio
    ><br /><br />
    {{else}}
    <div><strong>No archives currently.</strong></div>
    {{end}}
  </body>
</html>
