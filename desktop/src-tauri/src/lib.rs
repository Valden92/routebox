use std::io::{Read, Write};
use std::net::TcpStream;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::time::Duration;
use tauri::Manager;

const API_ADDR: &str = "127.0.0.1:47891";
/// Согласовано с desktop/src/status-ui.ts (MIN_API_VERSION).
const MIN_API_VERSION: u64 = 7;

struct DaemonState(Mutex<Option<Child>>);

fn read_api_token(path: &std::path::Path) -> Result<String, String> {
    let metadata = std::fs::symlink_metadata(path).map_err(|_| "API credential unavailable")?;
    if !metadata.is_file() || metadata.file_type().is_symlink() {
        return Err("Invalid API credential file".into());
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        if metadata.permissions().mode() & 0o077 != 0 {
            return Err("API credential must have private permissions".into());
        }
    }
    let token = std::fs::read_to_string(path).map_err(|_| "API credential unavailable")?;
    let token = token.trim();
    if token.len() != 64 || !token.bytes().all(|c| c.is_ascii_hexdigit()) {
        return Err("Invalid API credential".into());
    }
    Ok(token.to_owned())
}

fn api_token_path(config_home: Option<PathBuf>, home: Option<PathBuf>) -> Result<PathBuf, String> {
    let base = config_home
        .filter(|p| !p.as_os_str().is_empty())
        .or_else(|| home.map(|p| p.join(".config")))
        .ok_or("Config directory unavailable")?;
    Ok(base.join("vpn-router/.api-token"))
}

#[tauri::command]
fn local_api_token() -> Result<String, String> {
    let path = api_token_path(
        std::env::var_os("XDG_CONFIG_HOME").map(PathBuf::from),
        std::env::var_os("HOME").map(PathBuf::from),
    )?;
    read_api_token(&path)
}

fn daemon_path() -> PathBuf {
    if cfg!(debug_assertions) {
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../bin/vpn-router-daemon")
    } else {
        PathBuf::from("vpn-router-daemon")
    }
}

fn api_running() -> bool {
    TcpStream::connect_timeout(
        &API_ADDR.parse().expect("valid API_ADDR"),
        Duration::from_millis(400),
    )
    .is_ok()
}

/// Читает HTTP-ответ без требования мгновенного close (таймаут read не = ошибка).
fn http_get(path: &str) -> Option<String> {
    let mut stream =
        TcpStream::connect_timeout(&API_ADDR.parse().ok()?, Duration::from_secs(2)).ok()?;
    let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
    let _ = stream.set_write_timeout(Some(Duration::from_secs(2)));
    stream
        .write_all(
            format!("GET {path} HTTP/1.1\r\nHost: {API_ADDR}\r\nConnection: close\r\n\r\n")
                .as_bytes(),
        )
        .ok()?;

    let mut buf = Vec::new();
    let mut tmp = [0u8; 4096];
    loop {
        match stream.read(&mut tmp) {
            Ok(0) => break,
            Ok(n) => {
                buf.extend_from_slice(&tmp[..n]);
                // Достаточно для /api/version|/api/health
                if buf.len() > 8 && buf.windows(4).any(|w| w == b"\r\n\r\n") && buf.len() > 64 {
                    // если есть Content-Length — дочитаем тело
                    if let Some(body_at) = find_header_end(&buf) {
                        if body_complete(&buf, body_at) {
                            break;
                        }
                    }
                }
                if buf.len() > 64 * 1024 {
                    break;
                }
            }
            Err(e)
                if e.kind() == std::io::ErrorKind::WouldBlock
                    || e.kind() == std::io::ErrorKind::TimedOut =>
            {
                break;
            }
            Err(_) => return None,
        }
    }
    if buf.is_empty() {
        return None;
    }
    let text = String::from_utf8_lossy(&buf);
    let body = text.split("\r\n\r\n").nth(1)?.trim_end_matches('\0').trim();
    if body.is_empty() {
        return None;
    }
    Some(body.to_string())
}

fn find_header_end(buf: &[u8]) -> Option<usize> {
    buf.windows(4).position(|w| w == b"\r\n\r\n").map(|i| i + 4)
}

fn body_complete(buf: &[u8], body_at: usize) -> bool {
    let headers = std::str::from_utf8(&buf[..body_at]).unwrap_or("");
    for line in headers.lines() {
        let lower = line.to_ascii_lowercase();
        if let Some(rest) = lower.strip_prefix("content-length:") {
            if let Ok(n) = rest.trim().parse::<usize>() {
                return buf.len().saturating_sub(body_at) >= n;
            }
        }
    }
    // без Content-Length считаем достаточным небольшой JSON
    buf.len().saturating_sub(body_at) >= 8
}

fn api_version() -> Option<u64> {
    let body = http_get("/api/version")?;
    // на случай trailing whitespace / нескольких JSON
    let body = body.lines().next()?.trim();
    let v: serde_json::Value = serde_json::from_str(body).ok()?;
    v.get("apiVersion")?.as_u64()
}

fn api_health_ok() -> bool {
    http_get("/api/health")
        .map(|b| b.contains("ok"))
        .unwrap_or(false)
}

/// true = можно пользоваться; false = порта нет / мёртвый процесс.
/// None version при живом health — не считаем «устаревшим» (флейк парсинга).
enum ApiProbe {
    Ok,
    Outdated(u64),
    Down,
}

fn probe_api() -> ApiProbe {
    if !api_running() {
        return ApiProbe::Down;
    }
    for attempt in 0..5 {
        if let Some(v) = api_version() {
            if v >= MIN_API_VERSION {
                return ApiProbe::Ok;
            }
            return ApiProbe::Outdated(v);
        }
        // порт открыт, version не распарсили — health?
        if api_health_ok() {
            std::thread::sleep(Duration::from_millis(200));
            if let Some(v) = api_version() {
                if v >= MIN_API_VERSION {
                    return ApiProbe::Ok;
                }
                return ApiProbe::Outdated(v);
            }
            // health ок, version глючит — не убиваем рабочий демон
            if attempt >= 2 {
                eprintln!(
                    "vpn-router: /api/health ок, /api/version не разобрали — оставляем демон"
                );
                return ApiProbe::Ok;
            }
        }
        std::thread::sleep(Duration::from_millis(150));
    }
    ApiProbe::Down
}

fn stop_stale_daemon() {
    let _ = Command::new("killall")
        .arg("vpn-router-daemon")
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    // Дождаться освобождения порта (иначе новый процесс не займёт :47891).
    for _ in 0..40 {
        if !api_running() {
            break;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    std::thread::sleep(Duration::from_millis(200));
}

fn daemon_log_path() -> PathBuf {
    config_dir().join("daemon-boot.log")
}

fn config_dir() -> PathBuf {
    if let Some(home) = std::env::var_os("HOME") {
        let p = PathBuf::from(home).join(".config/vpn-router");
        let _ = std::fs::create_dir_all(&p);
        return p;
    }
    PathBuf::from(".")
}

fn ensure_daemon() -> Option<Child> {
    match probe_api() {
        ApiProbe::Ok => {
            eprintln!("vpn-router: API ok (version {MIN_API_VERSION}+) at http://{API_ADDR}");
            return None;
        }
        ApiProbe::Outdated(v) => {
            eprintln!(
                "vpn-router: устаревший демон apiVersion={v} (нужен {MIN_API_VERSION}+) — перезапускаем"
            );
            stop_stale_daemon();
        }
        ApiProbe::Down => {}
    }

    let path = daemon_path();
    if !path.exists() {
        eprintln!("vpn-router: не найден бинарник демона: {}", path.display());
        return None;
    }

    let log_path = daemon_log_path();
    let log_file = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&log_path)
        .ok();

    let mut cmd = Command::new(&path);
    cmd.args(["--listen", API_ADDR]);
    cmd.stdout(Stdio::null());
    if let Some(f) = log_file {
        cmd.stderr(Stdio::from(f));
    } else {
        cmd.stderr(Stdio::null());
    }
    for (key, value) in std::env::vars() {
        cmd.env(key, value);
    }

    match cmd.spawn() {
        Ok(child) => {
            // до ~15 с: холодный старт Go / медленный диск
            for i in 0..75 {
                match probe_api() {
                    ApiProbe::Ok => {
                        eprintln!("vpn-router: демон запущен http://{API_ADDR}");
                        return Some(child);
                    }
                    ApiProbe::Outdated(v) => {
                        eprintln!(
                            "vpn-router: демон поднялся со старым apiVersion={v}, ждём/пересоберьте make build"
                        );
                    }
                    ApiProbe::Down => {}
                }
                if i == 20 {
                    eprintln!("vpn-router: ждём ответ API… (лог: {})", log_path.display());
                }
                std::thread::sleep(Duration::from_millis(200));
            }
            eprintln!(
                "vpn-router: демон не ответил /api/version за 15с — см. {}",
                log_path.display()
            );
            Some(child)
        }
        Err(e) => {
            eprintln!("vpn-router: не удалось запустить демон: {e}");
            None
        }
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![local_api_token])
        .plugin(tauri_plugin_opener::init())
        .setup(|app| {
            let child = ensure_daemon();
            app.manage(DaemonState(Mutex::new(child)));
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::Destroyed = event {
                if let Some(state) = window.app_handle().try_state::<DaemonState>() {
                    if let Ok(mut guard) = state.0.lock() {
                        if let Some(mut child) = guard.take() {
                            let _ = child.kill();
                        }
                    }
                }
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

#[cfg(all(test, unix))]
mod credential_tests {
    use super::{api_token_path, read_api_token};
    use std::os::unix::fs::{symlink, PermissionsExt};
    use std::path::PathBuf;

    struct TempDir(PathBuf);
    impl TempDir {
        fn new() -> Self {
            let stamp = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos();
            let path = std::env::temp_dir().join(format!(
                "routebox-rust-token-{}-{stamp}",
                std::process::id()
            ));
            std::fs::create_dir(&path).unwrap();
            Self(path)
        }
        fn token(&self, value: &str) -> PathBuf {
            let path = self.0.join(".api-token");
            std::fs::write(&path, value).unwrap();
            std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600)).unwrap();
            path
        }
    }
    impl Drop for TempDir {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    #[test]
    fn resolves_config_path_like_the_daemon() {
        let home = Some(PathBuf::from("/home/fixture"));
        assert_eq!(
            api_token_path(None, home.clone()).unwrap(),
            PathBuf::from("/home/fixture/.config/vpn-router/.api-token")
        );
        assert_eq!(
            api_token_path(Some(PathBuf::new()), home.clone()).unwrap(),
            PathBuf::from("/home/fixture/.config/vpn-router/.api-token")
        );
        assert_eq!(
            api_token_path(Some(PathBuf::from("/custom/config")), home).unwrap(),
            PathBuf::from("/custom/config/vpn-router/.api-token")
        );
        assert!(api_token_path(None, None).is_err());
    }

    #[test]
    fn reads_private_token_and_rotation() {
        let dir = TempDir::new();
        let path = dir.token(&"a".repeat(64));
        assert_eq!(read_api_token(&path).unwrap(), "a".repeat(64));
        dir.token(&"b".repeat(64));
        assert_eq!(read_api_token(&path).unwrap(), "b".repeat(64));
    }
    #[test]
    fn rejects_public_missing_invalid_or_symlink_files() {
        let dir = TempDir::new();
        let path = dir.token(&"a".repeat(64));
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o644)).unwrap();
        assert!(read_api_token(&path).is_err());
        dir.token("invalid");
        assert!(read_api_token(&path).is_err());
        let link = dir.0.join("link");
        symlink(&path, &link).unwrap();
        assert!(read_api_token(&link).is_err());
        std::fs::remove_file(&path).unwrap();
        assert!(read_api_token(&path).is_err());
    }
}
