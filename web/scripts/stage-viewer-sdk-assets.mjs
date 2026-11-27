import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const webRoot = path.resolve(__dirname, '..');
const sdkRoot = path.join(webRoot, 'node_modules', '@rl-replay', 'viewer-sdk');
const targetRoot = path.join(webRoot, 'public', 'viewer-sdk');

function copyRequiredFile(src, dest) {
  if (!fs.existsSync(src)) {
    throw new Error(`Missing required @rl-replay/viewer-sdk asset: ${src}`);
  }
  const stat = fs.statSync(src);
  if (!stat.isFile() || stat.size === 0) {
    throw new Error(`Invalid or empty @rl-replay/viewer-sdk asset: ${src}`);
  }
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  fs.copyFileSync(src, dest);
  return stat.size;
}

function copyRequiredDir(srcDir, destDir) {
  if (!fs.existsSync(srcDir) || !fs.statSync(srcDir).isDirectory()) {
    throw new Error(`Missing required @rl-replay/viewer-sdk directory: ${srcDir}`);
  }
  fs.mkdirSync(destDir, { recursive: true });
  let files = 0;
  let bytes = 0;
  for (const entry of fs.readdirSync(srcDir, { withFileTypes: true })) {
    const srcPath = path.join(srcDir, entry.name);
    const destPath = path.join(destDir, entry.name);
    if (entry.isDirectory()) {
      const sub = copyRequiredDir(srcPath, destPath);
      files += sub.files;
      bytes += sub.bytes;
    } else if (entry.isFile()) {
      bytes += copyRequiredFile(srcPath, destPath);
      files += 1;
    }
  }
  return { files, bytes };
}

function stageViewerSdkAssets() {
  if (!fs.existsSync(sdkRoot)) {
    throw new Error(
      `Cannot stage @rl-replay/viewer-sdk assets: package not installed at ${sdkRoot}. Run "npm install" inside web/.`,
    );
  }

  fs.rmSync(targetRoot, { recursive: true, force: true });
  fs.mkdirSync(targetRoot, { recursive: true });

  let totalFiles = 0;
  let totalBytes = 0;

  // 1. WASM parser binary
  const wasmSrc = path.join(sdkRoot, 'wasm', 'bin', 'rl_boxcars_v4.wasm');
  const wasmDest = path.join(targetRoot, 'wasm', 'rl_boxcars_v4.wasm');
  totalBytes += copyRequiredFile(wasmSrc, wasmDest);
  totalFiles += 1;

  // 2. Asset manifest
  const manifestSrc = path.join(sdkRoot, 'assets', 'manifest.json');
  const manifestDest = path.join(targetRoot, 'assets', 'manifest.json');
  totalBytes += copyRequiredFile(manifestSrc, manifestDest);
  totalFiles += 1;

  // 3. CC0 3D models & Draco decoder
  for (const subdir of ['models', 'draco']) {
    const res = copyRequiredDir(
      path.join(sdkRoot, 'assets', subdir),
      path.join(targetRoot, 'assets', subdir),
    );
    totalFiles += res.files;
    totalBytes += res.bytes;
  }

  const mb = (totalBytes / (1024 * 1024)).toFixed(2);
  console.log(
    `[stage-viewer-sdk] Staged ${totalFiles} files (${mb} MB) into public/viewer-sdk/`,
  );
}

stageViewerSdkAssets();
