use std::collections::HashMap;
use std::path::Path;

use super::items::{BaseItemDto, MediaStreamInfo, UserItemDataDto};

/// Format a database item row into an Emby-compatible BaseItemDto.
/// `item` is a sqlx::Row or a struct with the relevant fields.
pub fn format_item_dto(
    item: &ItemRow,
    server_id: &str,
    user_data: Option<&UserDataRow>,
) -> BaseItemDto {
    let mut dto = BaseItemDto {
        id: item.id.clone(),
        name: item.name.clone(),
        server_id: server_id.to_string(),
        item_type: item.item_type.clone(),
        sort_name: Some(item.sort_name.clone().unwrap_or_else(|| item.name.clone())),
        ..Default::default()
    };

    match item.item_type.as_str() {
        "Movie" | "Episode" => {
            dto.media_type = Some("Video".to_string());
            dto.is_folder = Some(false);
        }
        "Series" | "Season" | "CollectionFolder" => {
            dto.is_folder = Some(true);
        }
        _ => {}
    }

    dto.collection_type = item.collection_type.clone();
    dto.overview = item.overview.clone();
    dto.production_year = item.production_year;
    dto.premiere_date = item
        .premiere_date
        .as_ref()
        .map(|d| d.and_utc().to_rfc3339());
    dto.community_rating = item.community_rating;
    dto.official_rating = item.official_rating.clone();
    dto.run_time_ticks = item.runtime_ticks;
    dto.index_number = item.index_number;
    dto.parent_index_number = item.parent_index_number;
    dto.parent_id = item.parent_id.clone();
    dto.series_id = item.series_id.clone();
    dto.series_name = item.series_name.clone();
    dto.season_id = item.season_id.clone();
    dto.provider_ids = item.provider_ids.clone();

    // Path: prefer resolved_path, then try resolving .strm on-the-fly, then file_path
    let display_path = if let Some(ref rp) = item.resolved_path {
        Some(rp.clone())
    } else if let Some(ref fp) = item.file_path {
        if fp.ends_with(".strm") {
            resolve_strm_for_display(fp).or_else(|| Some(fp.clone()))
        } else {
            Some(fp.clone())
        }
    } else {
        None
    };
    dto.path = display_path.clone();

    // Container: use real container, not 'strm'
    if let Some(ref c) = item.container {
        if c != "strm" {
            dto.container = Some(c.clone());
        } else if let Some(ref p) = display_path {
            let ext = Path::new(p)
                .extension()
                .and_then(|e| e.to_str())
                .unwrap_or("mkv")
                .to_lowercase();
            dto.container = Some(ext);
        } else {
            dto.container = Some(c.clone());
        }
    }

    // Image tags
    let mut image_tags = HashMap::new();
    let series_item_id = item
        .series_fallback_id
        .clone()
        .or_else(|| item.series_id.clone())
        .or_else(|| item.parent_id.clone());

    if let Some(ref tag) = item.primary_image_tag {
        image_tags.insert("Primary".to_string(), tag.clone());
    }
    // For Episode/Season without own image: DON'T put series tag in ImageTags
    // Players use ImageTags.Primary + item's own ID to request images.
    // Instead, use SeriesPrimaryImageTag + SeriesId (set below in flat fields section).
    if !image_tags.is_empty() {
        dto.image_tags = Some(image_tags);
    }

    if let Some(ref tag) = item.backdrop_image_tag {
        dto.backdrop_image_tags = Some(vec![tag.clone()]);
    } else if item.item_type == "Episode" || item.item_type == "Season" {
        if let Some(ref tag) = item.series_backdrop_image_tag {
            // Don't set backdrop_image_tags on the episode itself
            // Use ParentBackdrop fields instead
            dto.parent_backdrop_item_id = series_item_id.clone();
            dto.parent_backdrop_image_tags = Some(vec![tag.clone()]);
        }
    }

    // Emby player compatibility: flat image fields for Episode/Season
    if item.item_type == "Episode" || item.item_type == "Season" {
        if let Some(ref tag) = item.series_primary_image_tag {
            dto.series_primary_image_tag = Some(tag.clone());
            if dto.series_primary_image_item_id.is_none() {
                dto.series_primary_image_item_id = series_item_id.clone();
            }
            // ParentPrimaryImage for season/episode navigation
            dto.parent_primary_image_item_id = series_item_id.clone();
            dto.parent_primary_image_tag = Some(tag.clone());
            // ParentThumb (players use this for episode thumbnails)
            dto.parent_thumb_item_id = series_item_id.clone();
            dto.parent_thumb_image_tag = Some(tag.clone());
        }
        if let Some(ref tag) = item.series_backdrop_image_tag {
            if dto.parent_backdrop_item_id.is_none() {
                dto.parent_backdrop_item_id = series_item_id.clone();
                dto.parent_backdrop_image_tags = Some(vec![tag.clone()]);
            }
        }
    }

    dto.child_count = item.child_count;
    dto.recursive_item_count = item.recursive_item_count;

    // User data
    dto.user_data = Some(if let Some(ud) = user_data {
        let position = ud.playback_position_ticks.unwrap_or(0);
        let play_count = ud.play_count.unwrap_or(0);
        let is_fav = ud.is_favorite.unwrap_or(false);
        let played = ud.played.unwrap_or(false);
        let percentage = if let Some(runtime) = dto.run_time_ticks {
            if runtime > 0 && position > 0 {
                Some((position as f64 / runtime as f64) * 100.0)
            } else {
                None
            }
        } else {
            None
        };
        UserItemDataDto {
            playback_position_ticks: position,
            play_count,
            is_favorite: is_fav,
            played,
            last_played_date: ud
                .last_played_date
                .as_ref()
                .map(|d| d.and_utc().to_rfc3339()),
            played_percentage: percentage,
        }
    } else {
        UserItemDataDto {
            playback_position_ticks: 0,
            play_count: 0,
            is_favorite: false,
            played: false,
            last_played_date: None,
            played_percentage: None,
        }
    });

    dto
}

/// Resolve a .strm file to its actual media path for display in DTOs.
/// Reads the first line of the strm file. For remote URLs returns as-is.
/// For local paths, applies mount path fixups.
fn resolve_strm_for_display(strm_path: &str) -> Option<String> {
    let content = std::fs::read_to_string(strm_path).ok()?;
    let line = content.lines().next()?.trim();
    if line.is_empty() || line.starts_with('#') {
        return None;
    }
    let mut resolved = line.to_string();
    if resolved.starts_with("http") {
        return Some(resolved);
    }
    if !resolved.starts_with('/') {
        return None;
    }
    if !Path::new(&resolved).exists() {
        let mnt_path = format!("/mnt{resolved}");
        if Path::new(&mnt_path).exists() {
            resolved = mnt_path;
        } else {
            let fixed = resolved.replacen("/CloudNAS", "/mnt/CloudNAS", 1);
            if fixed != resolved && Path::new(&fixed).exists() {
                resolved = fixed;
            }
        }
    }
    Some(resolved)
}

pub fn format_media_stream_dto(stream: &StreamRow) -> MediaStreamInfo {
    MediaStreamInfo {
        codec: stream.codec.clone().unwrap_or_default(),
        stream_type: stream.stream_type.clone(),
        index: stream.stream_index,
        language: stream.language.clone(),
        title: stream.title.clone(),
        is_default: stream.is_default.unwrap_or(false),
        is_forced: stream.is_forced.unwrap_or(false),
        width: stream.width,
        height: stream.height,
        bit_rate: stream.bit_rate,
        channels: stream.channels,
        sample_rate: stream.sample_rate,
        bit_depth: stream.bit_depth,
        pixel_format: stream.pixel_format.clone(),
        display_title: stream.display_title.clone(),
    }
}

// Row types that map to database columns
// These are used by format functions and will be populated by sqlx queries

#[derive(Debug, Clone, Default)]
pub struct ItemRow {
    pub id: String,
    pub name: String,
    pub item_type: String,
    pub sort_name: Option<String>,
    pub collection_type: Option<String>,
    pub overview: Option<String>,
    pub production_year: Option<i32>,
    pub premiere_date: Option<chrono::NaiveDateTime>,
    pub community_rating: Option<f64>,
    pub official_rating: Option<String>,
    pub runtime_ticks: Option<i64>,
    pub index_number: Option<i32>,
    pub parent_index_number: Option<i32>,
    pub parent_id: Option<String>,
    pub series_id: Option<String>,
    pub series_name: Option<String>,
    pub season_id: Option<String>,
    pub container: Option<String>,
    pub file_path: Option<String>,
    pub resolved_path: Option<String>,
    pub provider_ids: Option<serde_json::Value>,
    pub primary_image_tag: Option<String>,
    pub backdrop_image_tag: Option<String>,
    pub series_primary_image_tag: Option<String>,
    pub series_backdrop_image_tag: Option<String>,
    pub series_fallback_id: Option<String>,
    pub child_count: Option<i64>,
    pub recursive_item_count: Option<i64>,
}

#[derive(Debug, Clone, Default)]
pub struct UserDataRow {
    pub playback_position_ticks: Option<i64>,
    pub play_count: Option<i32>,
    pub is_favorite: Option<bool>,
    pub played: Option<bool>,
    pub last_played_date: Option<chrono::NaiveDateTime>,
}

#[derive(Debug, Clone, Default)]
pub struct StreamRow {
    pub codec: Option<String>,
    pub stream_type: String,
    pub stream_index: i32,
    pub language: Option<String>,
    pub title: Option<String>,
    pub is_default: Option<bool>,
    pub is_forced: Option<bool>,
    pub width: Option<i32>,
    pub height: Option<i32>,
    pub bit_rate: Option<i64>,
    pub channels: Option<i32>,
    pub sample_rate: Option<i32>,
    pub bit_depth: Option<i32>,
    pub pixel_format: Option<String>,
    pub display_title: Option<String>,
}
