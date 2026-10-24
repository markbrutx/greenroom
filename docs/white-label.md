# White-label

Greenroom can wear your company's name, logo and color. Three variables
change it; no rebuild is needed.

| Variable | Example | Effect |
| --- | --- | --- |
| `GREENROOM_NAME` | `Platform Team Tools` | Top bar, browser tab title, sign-in page. Up to 40 characters |
| `GREENROOM_LOGO` | `/etc/greenroom/logo.svg` | Top bar, sign-in page and favicon |
| `GREENROOM_ACCENT` | `#1f6feb` | Buttons, links, highlights and focus rings |

## Logo

Use a square image; it is shown at 28 px in the top bar and 56 px on the
sign-in page, with rounded corners. SVG works best. PNG, WebP, JPEG and ICO
work too. The file may be up to 512 KB and is read once at start, so restart
after replacing it.

Greenroom serves the logo at `/brand/logo` with a strict
Content-Security-Policy of its own, so scripts inside an SVG never run.

In Docker, mount the file read-only and point the variable at it:

```sh
docker run -v /srv/brand/logo.svg:/brand/logo.svg:ro -e GREENROOM_LOGO=/brand/logo.svg ...
```

In Kubernetes, put the logo into a ConfigMap and mount it the same way.

## Accent color

Give one color as `#rrggbb`. Greenroom generates `/brand.css` from it at
start:

- button text turns white or near black, whichever reads better on the
  accent;
- the link color in the light theme is darkened until it meets WCAG AA
  contrast (4.5:1) on the page background;
- the dark theme uses a lightened accent that meets AA on dark surfaces;
- hover, soft backgrounds and focus rings are mixed from the accent in CSS.

Without `GREENROOM_ACCENT`, the built-in green palette is used.

## Themes

Light and dark themes follow the system setting until a person picks one with
the moon or sun button; the choice is kept in their browser. Both themes are
only sets of CSS variables in `internal/web/static/app.css`. To change more
than the accent, edit those variables and rebuild; component styles never
mention a theme.

## Name in texts

The sign-in page says "Sign in to <name>" and the home page shows the name
above the greeting. The tool names (Retro) stay as they are.

## What does not change

Greenroom does not load fonts. It uses the system font stack of each device,
so it looks native on Windows, macOS, Linux, iOS and Android and works
offline.
