const { app, BrowserWindow } = require('electron');
const { readFileSync, writeFileSync, mkdtempSync } = require('node:fs');
const { join } = require('node:path');
const { tmpdir } = require('node:os');

app.commandLine.appendSwitch('force-device-scale-factor', '1');
app.setPath('userData', mkdtempSync(join(tmpdir(), 'kadran-wordmark-render-')));

app.whenReady().then(async () => {
  const svgPath = process.argv[2];
  const outputPath = process.argv[3];
  const svg = readFileSync(svgPath, 'utf8');
  const svgRoot = svg.match(/<svg\b[^>]*>/)?.[0] ?? '';
  const width = Number(svgRoot.match(/\bwidth="(\d+)"/)?.[1]);
  const height = Number(svgRoot.match(/\bheight="(\d+)"/)?.[1]);
  if (!Number.isInteger(width) || !Number.isInteger(height) || width < 1 || height < 1 || width > 5000 || height > 5000) {
    throw new Error('SVG must define pixel width and height within 1..5000');
  }
  const win = new BrowserWindow({
    width, height, show: false, transparent: true,
    backgroundColor: '#00000000', frame: false,
    webPreferences: { offscreen: true, nodeIntegration: false, contextIsolation: true, sandbox: true },
  });
  const html = '<!doctype html><meta charset="UTF-8"><style>html,body{margin:0;background:transparent;width:' + width + 'px;height:' + height + 'px;overflow:hidden}svg{display:block}</style>' + svg;
  await win.loadURL('data:text/html;base64,' + Buffer.from(html).toString('base64'));
  const captured = await win.webContents.capturePage();
  const size = captured.getSize();
  if (size.width !== width || size.height !== height) {
    throw new Error('Capture was clipped: expected ' + width + 'x' + height + ', received ' + size.width + 'x' + size.height);
  }
  writeFileSync(outputPath, captured.toPNG());
  console.log(JSON.stringify({ outputPath, size: captured.getSize() }));
  win.destroy();
  app.exit(0);
}).catch(error => { console.error(error); app.exit(1); });
