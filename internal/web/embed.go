package web

import "embed"

//go:embed recovery.js recovery.css diagnostics.js diagnostics.css index.html app.js manage.js reliability.js configuration.js conversation.js conversation.css applications.js product.css skill-bundles.js trace-model.js trace-process.js trace.js style.css trace.css
var Files embed.FS
