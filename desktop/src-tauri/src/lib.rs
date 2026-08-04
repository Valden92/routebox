use std::io::{Read, Write};
use std::net::TcpStream;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::time::Duration;
use tauri::Manager;

const API_ADDR: &str = "127.0.0.1:47891";
const MIN_API_VERSION: u64 = 2;

struct DaemonState(Mutex<Option<Child>>);

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
        Duration::from_millis(300),
    )
    .is_ok()
}

fn http_get(path: &str) -> Option<String> {
    let mut stream =
        TcpStream::connect_timeout(&API_ADDR.parse().ok()?, Duration::from_millis(500)).ok()?;
    stream
        .set_read_timeout(Some(Duration::from_millis(500)))
        .ok()?;
    stream
        .write_all(
            format!("GET {path} HTTP/1.1\r\nHost: {API_ADDR}\r\nConnection: close\r\n\r\n")
                .as_bytes(),
        )
        .ok()?;
    let mut buf = Vec::new();
    stream.read_to_end(&mut buf).ok()?;
    let text = String::from_utf8_lossy(&buf);
    text.split("\r\n\r\n").nth(1).map(|s| s.to_string())
}

fn api_version() -> Option<u64> {
    let body = http_get("/api/version")?;
    let v: serde_json::Value = serde_json::from_str(&body).ok()?;
    v.get("apiVersion")?.as_u64()
}

fn stop_stale_daemon() {
    let _ = Command::new("killall")
        .arg("vpn-router-daemon")
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    std::thread::sleep(Duration::from_millis(300));
}

fn ensure_daemon() -> Option<Child> {
    if api_running() {
        if api_version().unwrap_or(0) >= MIN_API_VERSION {
            eprintln!("vpn-router: API ok (version {MIN_API_VERSION}+) at http://{API_ADDR}");
            return None;
        }
        eprintln!("vpn-router: устаревший демон на :47891 — перезапускаем");
        stop_stale_daemon();
    }
    let path = daemon_path();
    if !path.exists() {
        eprintln!("vpn-router: не найден бинарник демона: {}", path.display());
        return None;
    }
    let mut cmd = Command::new(&path);
    cmd.args(["--listen", API_ADDR]);
    cmd.stdout(Stdio::null());
    cmd.stderr(Stdio::null());
    // Сессия пользователя: иначе nmcli не достучится до агента MFA NetworkManager
    for (key, value) in std::env::vars() {
        cmd.env(key, value);
    }
    match cmd.spawn() {
        Ok(child) => {
            for _ in 0..30 {
                if api_version().unwrap_or(0) >= MIN_API_VERSION {
                    eprintln!("vpn-router: демон запущен http://{API_ADDR}");
                    return Some(child);
                }
                std::thread::sleep(Duration::from_millis(100));
            }
            eprintln!("vpn-router: демон не ответил /api/version");
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
