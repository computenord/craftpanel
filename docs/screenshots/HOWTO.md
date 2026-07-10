# Screenshots for the README

Drop the four PNGs listed below into this folder, then uncomment the
`## Screenshots` block in the top level `README.md`. The filenames are what the
README expects.

## How to take them

Run the panel locally so nothing real is on screen:

```bash
go build -o craftpanel . && ./craftpanel -addr :8420 -data /tmp/cp-shots
```

Browser window at **1440 x 900**, page zoom 100 percent, UI language set with the
DE/EN switch in the header to whichever you want to present. Capture the browser
viewport only, no window chrome, no bookmarks bar, no desktop background.

Seed it with two servers so the dashboard does not look empty: create one Paper
server and one Vanilla server. Accept the EULA on exactly one of them, so the
EULA banner is visible but the panel still looks healthy.

## The four shots

| File | What to capture |
| ---- | --------------- |
| `dashboard.png` | The server list with two cards, one running and one stopped, and the amber EULA banner above the grid. This is the hero image, make it the good one. |
| `console.png` | A server's console tab with real output scrolled to the bottom, and something typed into the command input, for example `say hello`. |
| `files.png` | The file manager on a server root, showing `server.properties`, `eula.txt`, `server.jar` and the `world` folder. |
| `login.png` | The sign in screen. Leave the fields empty. |

## Please avoid

Real hostnames, real IP addresses, real usernames and anything from a production
box. The address line on the server detail page shows the host you are browsing,
so use `localhost` for the shots.
