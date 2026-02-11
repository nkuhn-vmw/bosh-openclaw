# OpenClaw Tile Resources

## Icon

The tile icon source is `icon.svg`. The OpsManager tile requires a PNG icon file (`icon.png`).

### Generating icon.png from icon.svg

Use one of the following methods to convert the SVG to a 256x256 PNG:

**ImageMagick:**
```bash
convert icon.svg icon.png
```

**Inkscape (CLI):**
```bash
inkscape icon.svg --export-type=png --export-filename=icon.png --export-width=256 --export-height=256
```

**librsvg (rsvg-convert):**
```bash
rsvg-convert -w 256 -h 256 icon.svg -o icon.png
```

The resulting `icon.png` should be committed to this directory so the tile build can reference it.
