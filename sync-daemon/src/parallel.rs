//! Worker pool for the transfers. The heavy work is network I/O (download/upload), so
//! parallelizing gives a real gain even with the SQLite index serialized behind a mutex.
//!
//! Uses `thread::scope` (threads whose lifetime is bound to the scope) so it can borrow
//! references -- `&Client`, `&Index`, `&Mutex<Stats>` -- without `Arc` or `'static`.

use std::sync::Mutex;

/// Runs `f` over all `items` with up to `workers` threads. Each worker pulls the next item from a
/// shared queue, so slow items do not block the others (unlike splitting into fixed chunks). With
/// `workers <= 1` it runs serially, without spawning threads.
pub fn for_each<T, F>(items: Vec<T>, workers: usize, f: F)
where
    T: Send,
    F: Fn(T) + Sync,
{
    if workers <= 1 || items.len() <= 1 {
        for it in items {
            f(it);
        }
        return;
    }
    let queue = Mutex::new(items.into_iter());
    let f = &f;
    let queue = &queue;
    std::thread::scope(|scope| {
        for _ in 0..workers.min(64) {
            scope.spawn(move || loop {
                // The lock only protects the queue -- it is released before running the work.
                let next = queue.lock().unwrap_or_else(|e| e.into_inner()).next();
                match next {
                    Some(item) => f(item),
                    None => break,
                }
            });
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    #[test]
    fn runs_every_item_exactly_once() {
        let seen = AtomicUsize::new(0);
        for_each((0..100).collect(), 8, |_i| {
            seen.fetch_add(1, Ordering::Relaxed);
        });
        assert_eq!(seen.load(Ordering::Relaxed), 100);
    }

    #[test]
    fn falls_back_to_serial() {
        let sum = AtomicUsize::new(0);
        for_each(vec![1usize, 2, 3], 1, |i| {
            sum.fetch_add(i, Ordering::Relaxed);
        });
        assert_eq!(sum.load(Ordering::Relaxed), 6);
    }
}
