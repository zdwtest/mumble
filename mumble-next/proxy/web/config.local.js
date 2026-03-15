// Local configuration for mumble-web
// Connects to our WebSocket proxy (no WebRTC)

let config = window.mumbleWebConfig

// Disable WebRTC - use WebSocket only (tunneled through our proxy)
config.defaults.webrtc = false

// Default connection settings
// Users can still change these in the connect dialog
config.defaults.address = window.location.hostname || 'localhost'
config.defaults.port = window.location.port || '8080'

// Theme (optional: 'MetroMumbleLight' or 'MetroMumbleDark')
config.defaults.theme = 'MetroMumbleLight'