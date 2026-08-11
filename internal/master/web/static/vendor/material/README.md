# Material Web (vendored)

Bundled from `@material/web` with esbuild for offline / zero-build deploy.

## Update

```powershell
$tmp = "$env:TEMP\md-vendor"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
Push-Location $tmp
npm init -y
npm install @material/web esbuild
# entry.js lists components used by WebUI; see repo history or rebuild from plan
# 在仓库根目录执行 outfile 指向本目录 material-web.js
npx esbuild entry.js --bundle --format=esm --outfile="<repo>/internal/master/web/static/vendor/material/material-web.js" --minify
Pop-Location
```

Components included: button variants, icon-button, outlined text-field/select, checkbox, switch, dialog, tabs, list, divider, icon, progress, chips, ripple, typescale.
