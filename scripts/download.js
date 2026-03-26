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
const isWindows = platform === 'win32';

// GitHub Actions 构建的文件名格式
const artifactName = isWindows
  ? `awesome-bot-windows-x64.zip`
  : `awesome-bot-${platform}-x64.tar.gz`;

const downloadUrl = `https://github.com/${repo}/releases/download/v${version}/${artifactName}`;

console.log(`Downloading ${artifactName}...`);

if (!fs.existsSync(binDir)) {
  fs.mkdirSync(binDir, { recursive: true });
}

const tempDir = path.join(os.tmpdir(), `awesome-bot-${Date.now()}`);
fs.mkdirSync(tempDir, { recursive: true });

const tempFile = path.join(tempDir, artifactName);

try {
  if (isWindows) {
    // Windows: 使用完整路径调用 PowerShell
    const psPath = `C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe`;
    const psCmd = `"${psPath}" -command "Invoke-WebRequest -Uri '${downloadUrl}' -OutFile '${tempFile}'"`;
    execSync(psCmd, { stdio: 'inherit' });

    // 解压
    const extractCmd = `"${psPath}" -command "Expand-Archive -Path '${tempFile}' -Destination '${binDir}' -Force"`;
    execSync(extractCmd, { stdio: 'inherit' });
  } else {
    // Unix: 使用 curl
    const curlCmd = `curl -L -o "${tempFile}" "${downloadUrl}"`;
    execSync(curlCmd, { stdio: 'inherit' });

    // 解压
    const extractCmd = `tar -xzf "${tempFile}" -C "${binDir}"`;
    execSync(extractCmd, { stdio: 'inherit' });

    // 设置执行权限
    fs.chmodSync(binPath, 0o755);
  }

  console.log(`Installed to ${binPath}`);
} finally {
  try {
    fs.rmSync(tempDir, { recursive: true, force: true });
  } catch (e) {}
}
