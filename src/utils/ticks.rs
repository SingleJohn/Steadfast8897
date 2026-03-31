const TICKS_PER_SECOND: i64 = 10_000_000;
const TICKS_PER_MS: i64 = 10_000;

pub fn seconds_to_ticks(seconds: f64) -> i64 {
    (seconds * TICKS_PER_SECOND as f64).round() as i64
}

pub fn ticks_to_seconds(ticks: i64) -> f64 {
    ticks as f64 / TICKS_PER_SECOND as f64
}

pub fn ms_to_ticks(ms: i64) -> i64 {
    ms * TICKS_PER_MS
}

pub fn ticks_to_ms(ticks: i64) -> i64 {
    ticks / TICKS_PER_MS
}
