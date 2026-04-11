PLUGIN_NAME := tvproxymux
BUILD_DIR := build
PATH := $(PATH):$(HOME)/go/bin

.PHONY: all generate build clean inspect test-step2 test-step3 test-step4 test-step5

all: build

generate:
	go generate

build: generate
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/libgst$(PLUGIN_NAME).so -buildmode c-shared .
	rm -f $(BUILD_DIR)/libgst$(PLUGIN_NAME).h

clean:
	rm -rf $(BUILD_DIR)

inspect: build
	GST_PLUGIN_PATH=./$(BUILD_DIR) gst-inspect-1.0 $(PLUGIN_NAME)

# Step 2: h264 video only
test-step2: build
	rm -f /tmp/step2.mp4
	timeout 10 bash -c 'GST_PLUGIN_PATH=./$(BUILD_DIR) gst-launch-1.0 -e \
		videotestsrc is-live=true num-buffers=150 ! x264enc speed-preset=ultrafast bitrate=2000 key-int-max=30 \
		! tvproxymux name=m ! filesink location=/tmp/step2.mp4' || true
	ffprobe -v quiet -show_entries stream=codec_name,codec_type -of compact /tmp/step2.mp4

# Step 3: h264 video + aac audio
test-step3: build
	rm -f /tmp/step3.mp4
	timeout 10 bash -c 'GST_PLUGIN_PATH=./$(BUILD_DIR) gst-launch-1.0 -e \
		videotestsrc is-live=true num-buffers=150 ! x264enc speed-preset=ultrafast bitrate=2000 key-int-max=30 ! m.video \
		audiotestsrc is-live=true num-buffers=150 ! faac ! m.audio \
		tvproxymux name=m ! filesink location=/tmp/step3.mp4' || true
	ffprobe -v quiet -show_entries stream=codec_name,codec_type -of compact /tmp/step3.mp4

# Step 4: h265 video + aac audio
test-step4: build
	rm -f /tmp/step4.mp4
	timeout 10 bash -c 'GST_PLUGIN_PATH=./$(BUILD_DIR) gst-launch-1.0 -e \
		videotestsrc is-live=true num-buffers=150 \
		! "video/x-raw,format=I420,width=640,height=480,framerate=25/1" \
		! vtenc_h265 bitrate=2000 realtime=true allow-frame-reordering=false ! m.video \
		audiotestsrc is-live=true num-buffers=150 ! faac ! m.audio \
		tvproxymux name=m ! filesink location=/tmp/step4.mp4' || true
	ffprobe -v quiet -show_entries stream=codec_name,codec_type -of compact /tmp/step4.mp4

# Step 5: MPEG-TS output
test-step5: build
	rm -f /tmp/step5.ts
	timeout 10 bash -c 'GST_PLUGIN_PATH=./$(BUILD_DIR) gst-launch-1.0 -e \
		videotestsrc is-live=true num-buffers=150 ! x264enc speed-preset=ultrafast bitrate=2000 key-int-max=30 ! m.video \
		audiotestsrc is-live=true num-buffers=150 ! faac ! m.audio \
		tvproxymux name=m output-format=mpegts ! filesink location=/tmp/step5.ts' || true
	ffprobe -v quiet -show_entries stream=codec_name,codec_type -of compact /tmp/step5.ts
