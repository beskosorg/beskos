//! Bandwidth limit (token bucket) shared by all transfers.
//!
//! A single `Throttle` is shared by the transfer threads, so the limit is GLOBAL (the user
//! configures "do not use more than X KB/s", not "X KB/s per file"). `rate = 0` turns it off.

use std::io::{self, Read};
use std::sync::Mutex;
use std::time::{Duration, Instant};

pub struct Throttle {
    /// Bytes per second. 0 = no limit (fast path, no locks).
    rate: u64,
    /// (instant of the last refill, tokens available in bytes)
    state: Mutex<(Instant, f64)>,
}

impl Throttle {
    pub fn new(bytes_per_sec: u64) -> Self {
        Throttle {
            rate: bytes_per_sec,
            state: Mutex::new((Instant::now(), bytes_per_sec as f64)),
        }
    }

    /// Blocks until it can "spend" `n` bytes. Refills the bucket according to elapsed time; if
    /// there are not enough tokens, sleeps exactly the missing time.
    pub fn take(&self, n: usize) {
        if self.rate == 0 || n == 0 {
            return;
        }
        let need = n as f64;
        loop {
            let wait = {
                let mut st = self.state.lock().unwrap_or_else(|e| e.into_inner());
                let now = Instant::now();
                let elapsed = now.duration_since(st.0).as_secs_f64();
                st.0 = now;
                // Refills, capped at 1 second of burst (avoids accumulating credit indefinitely).
                st.1 = (st.1 + elapsed * self.rate as f64).min(self.rate as f64);
                if st.1 >= need {
                    st.1 -= need;
                    return;
                }
                (need - st.1) / self.rate as f64
            };
            std::thread::sleep(Duration::from_secs_f64(wait.min(1.0)));
        }
    }
}

/// Reader that respects the bandwidth limit -- used both for downloads (response body) and
/// uploads (request body), so the limit applies in both directions.
pub struct ThrottledReader<'a, R: Read> {
    inner: R,
    throttle: &'a Throttle,
}

impl<'a, R: Read> ThrottledReader<'a, R> {
    pub fn new(inner: R, throttle: &'a Throttle) -> Self {
        ThrottledReader { inner, throttle }
    }
}

impl<R: Read> Read for ThrottledReader<'_, R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        // Caps the read request size so the delay is spread out (rather than in huge chunks),
        // keeping the throughput smooth.
        let cap = if self.throttle.rate == 0 {
            buf.len()
        } else {
            buf.len().min(64 * 1024)
        };
        let n = self.inner.read(&mut buf[..cap])?;
        self.throttle.take(n);
        Ok(n)
    }
}
