// Deploy-time configuration. This is the ONE file that differs between a
// same-origin deploy and a standalone one, so the UI stays a build-free folder:
// a static host overwrites it (or generates it from env at container start)
// instead of the bundle being rebuilt per environment.
//
//   wsURL: ""                            same origin as the page — /ws
//          "wss://api.example.com/ws"    API deployed elsewhere
//
// Precedence: ?ws= (one tab, for poking at another backend) > this > same origin.
window.AIRCOUNT_CONFIG = {
  wsURL: '',
};
