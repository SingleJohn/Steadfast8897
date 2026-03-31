use serde::{Deserialize, Serialize};
use std::process::Stdio;
use tokio::process::Command;

use crate::utils::ticks::seconds_to_ticks;

#[derive(Debug, Clone, Serialize)]
pub struct ProbeResult {
    pub duration_ticks: i64,
    pub streams: Vec<StreamInfo>,
    pub container: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct StreamInfo {
    pub index: i32,
    pub stream_type: String, // "Video" | "Audio" | "Subtitle"
    pub codec: String,
    pub language: Option<String>,
    pub title: Option<String>,
    pub is_default: bool,
    pub is_forced: bool,
    pub width: Option<i32>,
    pub height: Option<i32>,
    pub bit_rate: Option<i64>,
    pub channels: Option<i32>,
    pub sample_rate: Option<i32>,
    pub bit_depth: Option<i32>,
    pub pixel_format: Option<String>,
    pub display_title: Option<String>,
}

#[derive(Deserialize)]
struct FfprobeOutput {
    format: Option<FfprobeFormat>,
    streams: Option<Vec<FfprobeStream>>,
}

#[derive(Deserialize)]
struct FfprobeFormat {
    duration: Option<String>,
    format_name: Option<String>,
}

#[derive(Deserialize)]
struct FfprobeStream {
    index: Option<i32>,
    codec_type: Option<String>,
    codec_name: Option<String>,
    width: Option<i32>,
    height: Option<i32>,
    bit_rate: Option<String>,
    channels: Option<i32>,
    sample_rate: Option<String>,
    bits_per_raw_sample: Option<String>,
    pix_fmt: Option<String>,
    disposition: Option<FfprobeDisposition>,
    tags: Option<std::collections::HashMap<String, String>>,
}

#[derive(Deserialize)]
struct FfprobeDisposition {
    default: Option<i32>,
    forced: Option<i32>,
}

pub async fn probe_file(file_path: &str) -> Result<ProbeResult, String> {
    let output = Command::new("ffprobe")
        .args([
            "-v", "quiet",
            "-print_format", "json",
            "-show_format",
            "-show_streams",
            file_path,
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .output()
        .await
        .map_err(|e| format!("ffprobe exec error: {e}"))?;

    if !output.status.success() {
        return Err(format!("ffprobe failed with status: {}", output.status));
    }

    let data: FfprobeOutput = serde_json::from_slice(&output.stdout)
        .map_err(|e| format!("ffprobe JSON parse error: {e}"))?;

    let duration = data
        .format
        .as_ref()
        .and_then(|f| f.duration.as_ref())
        .and_then(|d| d.parse::<f64>().ok())
        .unwrap_or(0.0);

    let container = data
        .format
        .as_ref()
        .and_then(|f| f.format_name.as_ref())
        .map(|s| s.split(',').next().unwrap_or("").to_string())
        .unwrap_or_default();

    let streams: Vec<StreamInfo> = data
        .streams
        .unwrap_or_default()
        .iter()
        .filter_map(|s| {
            let codec_type = s.codec_type.as_deref()?;
            let stream_type = match codec_type {
                "video" => "Video",
                "audio" => "Audio",
                "subtitle" => "Subtitle",
                _ => return None,
            };

            let disp = s.disposition.as_ref();
            let tags = s.tags.as_ref();
            let lang = tags.and_then(|t| t.get("language")).cloned();
            let title = tags.and_then(|t| t.get("title")).cloned();
            let codec = s.codec_name.clone().unwrap_or_default();

            let mut info = StreamInfo {
                index: s.index.unwrap_or(0),
                stream_type: stream_type.to_string(),
                codec: codec.clone(),
                language: lang.clone(),
                title,
                is_default: disp.and_then(|d| d.default).unwrap_or(0) == 1,
                is_forced: disp.and_then(|d| d.forced).unwrap_or(0) == 1,
                width: None,
                height: None,
                bit_rate: None,
                channels: None,
                sample_rate: None,
                bit_depth: None,
                pixel_format: None,
                display_title: None,
            };

            match stream_type {
                "Video" => {
                    info.width = s.width;
                    info.height = s.height;
                    info.bit_rate = s.bit_rate.as_ref().and_then(|b| b.parse().ok());
                    info.pixel_format = s.pix_fmt.clone();
                    info.display_title = Some(format!(
                        "{} {}x{}",
                        codec.to_uppercase(),
                        s.width.unwrap_or(0),
                        s.height.unwrap_or(0)
                    ));
                }
                "Audio" => {
                    info.channels = s.channels;
                    info.sample_rate = s.sample_rate.as_ref().and_then(|r| r.parse().ok());
                    info.bit_rate = s.bit_rate.as_ref().and_then(|b| b.parse().ok());
                    info.bit_depth = s.bits_per_raw_sample.as_ref().and_then(|b| b.parse().ok());
                    let l = lang.as_deref().unwrap_or("und");
                    info.display_title = Some(format!(
                        "{} {}ch {}",
                        codec.to_uppercase(),
                        s.channels.unwrap_or(0),
                        l
                    ));
                }
                "Subtitle" => {
                    let l = lang.as_deref().unwrap_or("und");
                    info.display_title = Some(format!("{l} ({codec})"));
                }
                _ => {}
            }

            Some(info)
        })
        .collect();

    Ok(ProbeResult {
        duration_ticks: seconds_to_ticks(duration),
        streams,
        container,
    })
}
