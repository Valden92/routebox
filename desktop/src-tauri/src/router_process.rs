//! A stable reference to this Router BOX instance, not a kill-by-name operation.
//! pidfd prevents PID reuse from turning window close into a signal to another app.
#[cfg(target_os = "linux")]
mod platform {
    use serde::Deserialize;
    use std::fs::OpenOptions;
    use std::io::Read;
    use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
    use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
    use std::path::{Path, PathBuf};
    use std::time::{Duration, Instant};

    #[derive(Clone, Deserialize)]
    struct Identity {
        pid: i32,
        executable: PathBuf,
        started: String,
    }

    pub struct RouterProcess {
        identity: Identity,
        fd: OwnedFd,
    }

    fn process_identity(pid: i32) -> Result<Option<Identity>, String> {
        let root = PathBuf::from(format!("/proc/{pid}"));
        let stat = match std::fs::read_to_string(root.join("stat")) {
            Ok(stat) => stat,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(_) => return Err("Не удалось проверить процесс Router BOX".into()),
        };
        let fields: Vec<_> = stat
            .rsplit_once(')')
            .ok_or("Некорректное состояние процесса")?
            .1
            .split_whitespace()
            .collect();
        if fields.len() <= 19 {
            return Err("Некорректное состояние процесса".into());
        }
        if fields[0] == "Z" {
            return Ok(None);
        }
        let executable = match std::fs::read_link(root.join("exe")) {
            Ok(path) => PathBuf::from(path.to_string_lossy().trim_end_matches(" (deleted)")),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(_) => return Err("Не удалось проверить бинарник Router BOX".into()),
        };
        Ok(Some(Identity {
            pid,
            executable,
            started: fields[19].to_owned(),
        }))
    }

    fn open_pidfd(pid: i32) -> Result<Option<OwnedFd>, String> {
        if pid <= 1 || pid == std::process::id() as i32 {
            return Err("Некорректный владелец Router BOX".into());
        }
        // SAFETY: pidfd_open takes an integer PID and flags; no pointers are passed.
        let fd = unsafe { libc::syscall(libc::SYS_pidfd_open, pid, 0_u32) };
        if fd < 0 {
            if std::io::Error::last_os_error().raw_os_error() == Some(libc::ESRCH) {
                return Ok(None);
            }
            return Err(
                "Не удалось получить владение процессом Router BOX (нужен Linux 5.3+)".into(),
            );
        }
        // SAFETY: the successful syscall returned a new descriptor owned by this object.
        Ok(Some(unsafe { OwnedFd::from_raw_fd(fd as i32) }))
    }

    impl RouterProcess {
        pub fn capture() -> Result<Option<Self>, String> {
            // SAFETY: getuid has no arguments or preconditions.
            let uid = unsafe { libc::getuid() };
            Self::capture_record(
                &PathBuf::from(format!("/run/user/{uid}/routebox-daemon.lock")),
                uid,
            )
        }

        fn capture_record(path: &Path, uid: u32) -> Result<Option<Self>, String> {
            let file = match OpenOptions::new()
                .read(true)
                .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
                .open(path)
            {
                Ok(file) => file,
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
                Err(_) => return Err("Не удалось прочитать владельца Router BOX".into()),
            };
            let metadata = file
                .metadata()
                .map_err(|_| "Не удалось проверить владельца Router BOX")?;
            if !metadata.is_file()
                || metadata.uid() != uid
                || metadata.mode() & 0o077 != 0
                || metadata.nlink() != 1
                || metadata.len() > 4096
            {
                return Err("Недопустимый файл владельца Router BOX".into());
            }
            let identity: Identity = serde_json::from_reader(file.take(4096))
                .map_err(|_| "Некорректная запись владельца Router BOX")?;
            Self::capture_identity(identity)
        }

        fn capture_identity(identity: Identity) -> Result<Option<Self>, String> {
            if !identity.executable.is_absolute()
                || identity
                    .executable
                    .file_name()
                    .and_then(|name| name.to_str())
                    != Some("vpn-router-daemon")
                || identity.started.is_empty()
                || !identity.started.bytes().all(|c| c.is_ascii_digit())
            {
                return Err("Запись не принадлежит Router BOX".into());
            }
            let Some(fd) = open_pidfd(identity.pid)? else {
                return Ok(None);
            };
            let Some(current) = process_identity(identity.pid)? else {
                return Ok(None);
            };
            if current.executable != identity.executable || current.started != identity.started {
                return Err("Владелец Router BOX изменился; посторонний процесс сохранён".into());
            }
            Ok(Some(Self { identity, fd }))
        }

        pub fn capture_child(pid: u32, executable: &Path) -> Result<Option<Self>, String> {
            let pid = i32::try_from(pid).map_err(|_| "Некорректный PID демона")?;
            let Some(current) = process_identity(pid)? else {
                return Ok(None);
            };
            let expected = std::fs::canonicalize(executable)
                .map_err(|_| "Не удалось проверить запускаемый демон")?;
            if current.executable != expected {
                return Err("Запущен неожиданный процесс вместо Router BOX".into());
            }
            Self::capture_identity(current)
        }

        pub fn stop(&self, timeout: Duration) -> Result<(), String> {
            if self.exited()? {
                return Ok(());
            }
            let Some(current) = process_identity(self.identity.pid)? else {
                return Ok(());
            };
            if current.executable != self.identity.executable
                || current.started != self.identity.started
            {
                return Err("Процесс изменился; сигнал завершения не отправлен".into());
            }
            // SAFETY: fd is owned and valid; a null siginfo asks the kernel for
            // normal SIGTERM semantics. No process-group or SIGKILL is used.
            let result = unsafe {
                libc::syscall(
                    libc::SYS_pidfd_send_signal,
                    self.fd.as_raw_fd(),
                    libc::SIGTERM,
                    std::ptr::null::<libc::siginfo_t>(),
                    0_u32,
                )
            };
            if result < 0 {
                if std::io::Error::last_os_error().raw_os_error() == Some(libc::ESRCH) {
                    return Ok(());
                }
                return Err("Не удалось отправить корректное завершение Router BOX".into());
            }
            let deadline = Instant::now() + timeout;
            loop {
                let remaining = deadline.saturating_duration_since(Instant::now());
                let mut descriptor = libc::pollfd {
                    fd: self.fd.as_raw_fd(),
                    events: libc::POLLIN,
                    revents: 0,
                };
                // SAFETY: descriptor is a valid writable pollfd for this call.
                let result = unsafe {
                    libc::poll(
                        &mut descriptor,
                        1,
                        remaining.as_millis().min(i32::MAX as u128) as i32,
                    )
                };
                if result > 0 && descriptor.revents & (libc::POLLIN | libc::POLLHUP) != 0 {
                    return Ok(());
                }
                if result == 0 {
                    return Err(
                        "Демон ещё не завершился. Выполните make stop и повторите закрытие".into(),
                    );
                }
                if result < 0
                    && std::io::Error::last_os_error().kind() == std::io::ErrorKind::Interrupted
                {
                    continue;
                }
                return Err("Не удалось подтвердить завершение Router BOX".into());
            }
        }

        fn exited(&self) -> Result<bool, String> {
            let mut descriptor = libc::pollfd {
                fd: self.fd.as_raw_fd(),
                events: libc::POLLIN,
                revents: 0,
            };
            // SAFETY: descriptor is valid; zero timeout only reads exit readiness.
            let result = unsafe { libc::poll(&mut descriptor, 1, 0) };
            if result < 0 {
                return Err("Не удалось проверить завершение Router BOX".into());
            }
            Ok(result > 0 && descriptor.revents & (libc::POLLIN | libc::POLLHUP) != 0)
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        use std::os::unix::fs::{symlink, PermissionsExt};
        use std::process::{Child, Command};
        use std::sync::Mutex;

        static SPAWN_FIXTURE: Mutex<()> = Mutex::new(());

        struct Fixture {
            dir: PathBuf,
            child: Child,
            identity: Identity,
        }
        impl Fixture {
            fn new(pause: bool) -> Self {
                let _spawn_guard = SPAWN_FIXTURE.lock().unwrap();
                let stamp = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap()
                    .as_nanos();
                let dir = std::env::temp_dir().join(format!(
                    "routebox-close-test-{}-{stamp}",
                    std::process::id()
                ));
                std::fs::create_dir(&dir).unwrap();
                let executable = dir.join("vpn-router-daemon");
                // This is a harmless sleep fixture, never a router or a TUN process.
                std::fs::copy("/bin/sleep", &executable).unwrap();
                let mut command = Command::new(&executable);
                command.arg("30");
                let child = command.spawn().unwrap();
                let identity = process_identity(child.id() as i32).unwrap().unwrap();
                if pause {
                    // SAFETY: this is exclusively our harmless child fixture.
                    assert_eq!(unsafe { libc::kill(identity.pid, libc::SIGSTOP) }, 0);
                    let deadline = Instant::now() + Duration::from_secs(2);
                    loop {
                        let stat = std::fs::read_to_string(format!("/proc/{}/stat", identity.pid))
                            .unwrap();
                        if stat
                            .rsplit_once(')')
                            .unwrap()
                            .1
                            .trim_start()
                            .starts_with('T')
                        {
                            break;
                        }
                        assert!(Instant::now() < deadline, "fixture did not pause");
                        std::thread::sleep(Duration::from_millis(1));
                    }
                }
                Self {
                    dir,
                    child,
                    identity,
                }
            }
            fn record(&self) -> PathBuf {
                let record = self.dir.join("owner");
                let data = serde_json::json!({"pid":self.identity.pid,"executable":self.identity.executable,"started":self.identity.started});
                std::fs::write(&record, data.to_string()).unwrap();
                std::fs::set_permissions(&record, std::fs::Permissions::from_mode(0o600)).unwrap();
                record
            }
        }
        impl Drop for Fixture {
            fn drop(&mut self) {
                let _ = self.child.kill();
                let _ = self.child.wait();
                let _ = std::fs::remove_dir_all(&self.dir);
            }
        }
        #[test]
        fn window_close_uses_sigterm_and_waits_for_the_exact_owner() {
            let mut fixture = Fixture::new(false);
            let owner = RouterProcess::capture_record(&fixture.record(), unsafe { libc::getuid() })
                .unwrap()
                .unwrap();
            owner.stop(Duration::from_secs(2)).unwrap();
            use std::os::unix::process::ExitStatusExt;
            assert_eq!(fixture.child.wait().unwrap().signal(), Some(libc::SIGTERM));
            owner.stop(Duration::from_millis(10)).unwrap();
        }
        #[test]
        fn reused_identity_is_rejected_without_stopping_the_process() {
            let mut fixture = Fixture::new(false);
            let mut wrong = fixture.identity.clone();
            wrong.started.push('0');
            assert!(RouterProcess::capture_identity(wrong).is_err());
            assert!(fixture.child.try_wait().unwrap().is_none());
        }
        #[test]
        fn refuses_unrelated_executable_and_public_or_symlink_records() {
            let mut fixture = Fixture::new(false);
            let mut wrong = fixture.identity.clone();
            wrong.executable = PathBuf::from("/bin/sleep");
            assert!(RouterProcess::capture_identity(wrong).is_err());
            let record = fixture.record();
            let link = fixture.dir.join("link");
            symlink(&record, &link).unwrap();
            let uid = unsafe { libc::getuid() };
            assert!(RouterProcess::capture_record(&link, uid).is_err());
            std::fs::set_permissions(&record, std::fs::Permissions::from_mode(0o644)).unwrap();
            assert!(RouterProcess::capture_record(&record, uid).is_err());
            assert!(fixture.child.try_wait().unwrap().is_none());
        }
        #[test]
        fn timeout_does_not_force_kill_a_slow_owner() {
            let mut fixture = Fixture::new(true);
            let owner = RouterProcess::capture_identity(fixture.identity.clone())
                .unwrap()
                .unwrap();
            assert!(owner.stop(Duration::from_millis(50)).is_err());
            assert!(fixture.child.try_wait().unwrap().is_none());
        }
        #[test]
        fn child_capture_does_not_require_an_external_pid_file() {
            let fixture = Fixture::new(false);
            let owner =
                RouterProcess::capture_child(fixture.child.id(), &fixture.identity.executable)
                    .unwrap()
                    .unwrap();
            owner.stop(Duration::from_secs(2)).unwrap();
        }
    }
}

#[cfg(target_os = "linux")]
pub use platform::RouterProcess;

#[cfg(not(target_os = "linux"))]
pub struct RouterProcess;
#[cfg(not(target_os = "linux"))]
impl RouterProcess {
    pub fn capture() -> Result<Option<Self>, String> {
        Err("Корректное завершение реализовано для Linux".into())
    }
    pub fn capture_child(_: u32, _: &std::path::Path) -> Result<Option<Self>, String> {
        Self::capture()
    }
    pub fn stop(&self, _: std::time::Duration) -> Result<(), String> {
        Err("Корректное завершение реализовано для Linux".into())
    }
}
