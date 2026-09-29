package web

import "embed"

//go:embed index.html app.js manage.js reliability.js trace-model.js trace.js style.css trace.css
var Files embed.FS
