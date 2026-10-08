package web

import "embed"

//go:embed locales-en.json locales-en.json i18n.js setup.js layout.js layout.css collaboration.html collaboration.js collaboration.css library.js library.css builds.js builds.css users.js users.css member.html member.js member.css images.js images.css environments.js environments.css application-keys.js schedules.js tasks.js tasks.css recovery.js recovery.css diagnostics.js diagnostics.css index.html app.js manage.js reliability.js configuration.js conversation.js conversation.css applications.js product.css skill-bundles.js trace-model.js trace-process.js trace.js style.css trace.css
var Files embed.FS
