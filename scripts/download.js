const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const repo = 'enter33/AwesomeBot';
const binDir = path.join(__dirname, '..', 'bin');
const binName = os.platform() === 'win32' ? 'awesome-bot.exe' : 'awesome-bot';
const binPath = path.join(binDir, binName);

// 如果二进制已存在，直接退出
if (fs.existsSync(binPath)) {
  return;
}

// 获取版本：如果已安装则使用已安装版本，否则使用 latest
function getVersion() {
  try {
    // 尝试从 node_modules 获取版本
    const pkgPath = path.join(__dirname, '..', 'package.json');
    if (fs.existsSync(pkgPath)) {
      const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));
      return pkg.version;
    }
  } catch (e) {}
  return 'latest';
}

const version = getVersion();
const platform = os.platform();
const arch = os.arch() ===('arm64') ? 'arm64' : 'x64';

const ext = platform === 'win32' ? 'zip' : 'tar.gz';
const downloadName = platform === 'win32'
  ? `awesome-bot-${version}-win-${arch}.zip`
  : `awesome-bot-${version}-${platform}-${arch}.tar.gz`;

const downloadUrl = `https://github.com/${repo}/releases/download/v${version}/${downloadName}`;

console.log(`Downloading ${downloadName}...`);

// 确保 bin 目录存在
if (!fs.existsSync(binDir)) {
  fs.mkdirSync(binDir, { recursive: true });
}

// 下载并解压
const tempDir = path.join(os.tmpdir(), `awesome-bot-${Date.now()}`);

try {
  // 使用 curl 下载
  const curlCmd = `curl -L --output-dir "${tempDir}" "${downloadUrl}"`;
  execSync(curlCmd, { stdio: 'inherit' });

  // 解压
  const extractCmd = platform === 'win32'
    ? `powershell -command "Expand-Archive -Path '${path.join(tempDir, downloadName)}' -Destination '${binDir}' -Force"`
    : `tar -xzf "${path.join(tempDir, downloadName)}" -C "${binDir}"`;
  execSync(extractCmd, { stdio: 'inherit' });

  // 设置执行权限 (Linux/macOS)
  if (platform !== 'win32') {
    fs.chmodSync(binPath, 0o755);
  }

  console.log(`Installed to ${binPath}`);
} finally {
  // 清理临时文件
  try {
    fs.rmSync(tempDir, { recursive: true, force: true });
  } catch (e) {}
}
