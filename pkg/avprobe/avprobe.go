package avprobe

/*
#cgo pkg-config: libavformat libavcodec libavutil
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/pixdesc.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	int ok;
	char *video_codec;
	int width;
	int height;
	int framerate_num;
	int framerate_denom;
	int interlaced;
	int video_bit_depth;
	char *pix_fmt;
	char *video_profile;
	char *color_space;
	char *color_transfer;
	char *color_primaries;
	char *field_order;
	char *video_bitrate;
	char *audio_codec;
	int channels;
	int sample_rate;
	char *audio_lang;
	char *audio_bitrate;
	double duration;
	double video_start_time;
	double video_duration;
	double audio_start_time;
	double audio_duration;
	int is_vod;
	char *format_name;
	char *error;
} CProbeInfo;

static CProbeInfo c_probe(const char *url, const char *user_agent) {
	CProbeInfo info = {0};
	AVFormatContext *fmt_ctx = NULL;
	AVDictionary *opts = NULL;

	av_dict_set(&opts, "analyzeduration", "2000000", 0);
	av_dict_set(&opts, "probesize", "2000000", 0);
	if (user_agent && user_agent[0]) {
		av_dict_set(&opts, "user_agent", user_agent, 0);
	}

	int ret = avformat_open_input(&fmt_ctx, url, NULL, &opts);
	av_dict_free(&opts);
	if (ret < 0) {
		char errbuf[128];
		av_strerror(ret, errbuf, sizeof(errbuf));
		info.error = strdup(errbuf);
		return info;
	}

	ret = avformat_find_stream_info(fmt_ctx, NULL);
	if (ret < 0) {
		char errbuf[128];
		av_strerror(ret, errbuf, sizeof(errbuf));
		info.error = strdup(errbuf);
		avformat_close_input(&fmt_ctx);
		return info;
	}

	info.ok = 1;
	if (fmt_ctx->duration > 0) {
		info.duration = (double)fmt_ctx->duration / AV_TIME_BASE;
		info.is_vod = 1;
	}
	info.format_name = strdup(fmt_ctx->iformat->name);

	for (unsigned i = 0; i < fmt_ctx->nb_streams; i++) {
		AVCodecParameters *par = fmt_ctx->streams[i]->codecpar;

		if (par->codec_type == AVMEDIA_TYPE_VIDEO && !info.video_codec) {
			const AVCodecDescriptor *desc = avcodec_descriptor_get(par->codec_id);
			info.video_codec = desc ? strdup(desc->name) : strdup("unknown");
			info.width = par->width;
			info.height = par->height;

			AVRational fr = fmt_ctx->streams[i]->r_frame_rate;
			if (fr.num == 0 || fr.den == 0) {
				fr = fmt_ctx->streams[i]->avg_frame_rate;
			}
			info.framerate_num = fr.num;
			info.framerate_denom = fr.den;

			info.interlaced = (par->field_order != AV_FIELD_PROGRESSIVE &&
			                   par->field_order != AV_FIELD_UNKNOWN) ? 1 : 0;

			const AVPixFmtDescriptor *pix_desc = av_pix_fmt_desc_get((enum AVPixelFormat)par->format);
			if (pix_desc) {
				if (pix_desc->nb_components > 0) {
					info.video_bit_depth = pix_desc->comp[0].depth;
				}
			}

			const char *cs = av_color_space_name(par->color_space);
			if (cs && cs[0]) info.color_space = strdup(cs);

			const char *ct = av_color_transfer_name(par->color_trc);
			if (ct && ct[0]) info.color_transfer = strdup(ct);

			const char *cp = av_color_primaries_name(par->color_primaries);
			if (cp && cp[0]) info.color_primaries = strdup(cp);

			switch (par->field_order) {
				case AV_FIELD_PROGRESSIVE: info.field_order = strdup("progressive"); break;
				case AV_FIELD_TT: info.field_order = strdup("tt"); break;
				case AV_FIELD_BB: info.field_order = strdup("bb"); break;
				case AV_FIELD_TB: info.field_order = strdup("tb"); break;
				case AV_FIELD_BT: info.field_order = strdup("bt"); break;
				default: break;
			}

			const char *pix = av_get_pix_fmt_name((enum AVPixelFormat)par->format);
			if (pix) info.pix_fmt = strdup(pix);

			if (par->profile >= 0) {
				const char *prof = avcodec_profile_name(par->codec_id, par->profile);
				if (prof) info.video_profile = strdup(prof);
			}

			if (par->bit_rate > 0) {
				char br[32];
				snprintf(br, sizeof(br), "%lld", (long long)par->bit_rate);
				info.video_bitrate = strdup(br);
			}

			if (fmt_ctx->streams[i]->start_time != AV_NOPTS_VALUE) {
				info.video_start_time = (double)fmt_ctx->streams[i]->start_time * av_q2d(fmt_ctx->streams[i]->time_base);
			}
			if (fmt_ctx->streams[i]->duration > 0) {
				info.video_duration = (double)fmt_ctx->streams[i]->duration * av_q2d(fmt_ctx->streams[i]->time_base);
			}
		}

		if (par->codec_type == AVMEDIA_TYPE_AUDIO && !info.audio_codec) {
			const AVCodecDescriptor *desc = avcodec_descriptor_get(par->codec_id);
			info.audio_codec = desc ? strdup(desc->name) : strdup("unknown");
			info.channels = par->ch_layout.nb_channels;
			info.sample_rate = par->sample_rate;

			AVDictionaryEntry *lang = av_dict_get(fmt_ctx->streams[i]->metadata, "language", NULL, 0);
			if (lang) info.audio_lang = strdup(lang->value);

			if (fmt_ctx->streams[i]->start_time != AV_NOPTS_VALUE) {
				info.audio_start_time = (double)fmt_ctx->streams[i]->start_time * av_q2d(fmt_ctx->streams[i]->time_base);
			}
			if (fmt_ctx->streams[i]->duration > 0) {
				info.audio_duration = (double)fmt_ctx->streams[i]->duration * av_q2d(fmt_ctx->streams[i]->time_base);
			}

			if (par->bit_rate > 0) {
				char br[32];
				snprintf(br, sizeof(br), "%lld", (long long)par->bit_rate);
				info.audio_bitrate = strdup(br);
			}
		}
	}

	avformat_close_input(&fmt_ctx);
	return info;
}

static void free_probe_info(CProbeInfo *info) {
	if (info->video_codec) free(info->video_codec);
	if (info->pix_fmt) free(info->pix_fmt);
	if (info->video_profile) free(info->video_profile);
	if (info->color_space) free(info->color_space);
	if (info->color_transfer) free(info->color_transfer);
	if (info->color_primaries) free(info->color_primaries);
	if (info->field_order) free(info->field_order);
	if (info->video_bitrate) free(info->video_bitrate);
	if (info->audio_codec) free(info->audio_codec);
	if (info->audio_lang) free(info->audio_lang);
	if (info->audio_bitrate) free(info->audio_bitrate);
	if (info->format_name) free(info->format_name);
	if (info->error) free(info->error);
}
*/
import "C"

import (
	"context"
	"fmt"
	"strconv"
	"unsafe"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func Probe(ctx context.Context, url, userAgent string) (*media.ProbeResult, error) {
	curl := C.CString(url)
	defer C.free(unsafe.Pointer(curl))

	cua := C.CString(userAgent)
	defer C.free(unsafe.Pointer(cua))

	info := C.c_probe(curl, cua)
	defer C.free_probe_info(&info)

	if info.error != nil {
		return nil, fmt.Errorf("avprobe: %s", C.GoString(info.error))
	}
	if info.ok == 0 {
		return &media.ProbeResult{}, nil
	}

	result := &media.ProbeResult{
		Duration:   float64(info.duration),
		IsVOD:      info.is_vod != 0,
		Width:      int(info.width),
		Height:     int(info.height),
		HasVideo:   info.video_codec != nil,
		FormatName: media.NormalizeContainer(C.GoString(info.format_name)),
	}

	if info.video_codec != nil {
		fps := ""
		if info.framerate_denom > 0 {
			fpsVal := float64(info.framerate_num) / float64(info.framerate_denom)
			if fpsVal > 0 && fpsVal < 300 {
				if fpsVal == float64(int(fpsVal)) {
					fps = strconv.Itoa(int(fpsVal))
				} else {
					fps = strconv.FormatFloat(fpsVal, 'f', 2, 64)
				}
			}
		}

		result.Video = &media.VideoInfo{
			Codec:      C.GoString(info.video_codec),
			Profile:    goStringOrEmpty(info.video_profile),
			PixFmt:     goStringOrEmpty(info.pix_fmt),
			BitDepth:   int(info.video_bit_depth),
			Interlaced:     info.interlaced != 0,
			ColorSpace:     goStringOrEmpty(info.color_space),
			ColorTransfer:  goStringOrEmpty(info.color_transfer),
			ColorPrimaries: goStringOrEmpty(info.color_primaries),
			FieldOrder:     goStringOrEmpty(info.field_order),
			FPS:        fps,
			BitRate:    goStringOrEmpty(info.video_bitrate),
			StartTime:  float64(info.video_start_time),
			Duration:   float64(info.video_duration),
		}
	}

	if info.audio_codec != nil {
		result.AudioTracks = []media.AudioTrack{{
			Index:      0,
			Language:   goStringOrEmpty(info.audio_lang),
			Codec:      C.GoString(info.audio_codec),
			SampleRate: strconv.Itoa(int(info.sample_rate)),
			Channels:   int(info.channels),
			BitRate:    goStringOrEmpty(info.audio_bitrate),
			StartTime:  float64(info.audio_start_time),
			Duration:   float64(info.audio_duration),
		}}
	}

	return result, nil
}

func goStringOrEmpty(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}
