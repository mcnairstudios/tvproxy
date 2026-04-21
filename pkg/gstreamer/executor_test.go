package gstreamer

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutor_BuildValidSpec(t *testing.T) {
	log := zerolog.Nop()
	exec := NewExecutor(log)

	spec := PipelineSpec{}
	spec.AddElement("src", "fakesrc", Props{"num-buffers": 1})
	spec.AddElement("sink", "fakesink", Props{"sync": false})
	spec.Link("src", "sink")

	pipeline, err := exec.Build(spec)
	require.NoError(t, err)
	require.NotNil(t, pipeline)
	exec.Stop(pipeline)
}

func TestExecutor_BuildUnknownFactory(t *testing.T) {
	log := zerolog.Nop()
	exec := NewExecutor(log)

	spec := PipelineSpec{}
	spec.AddElement("bad", "nonexistent_element_xyz", nil)

	pipeline, err := exec.Build(spec)
	assert.Error(t, err)
	assert.Nil(t, pipeline)
	assert.Contains(t, err.Error(), "not available")
}

func TestExecutor_BuildMissingLinkTarget(t *testing.T) {
	log := zerolog.Nop()
	exec := NewExecutor(log)

	spec := PipelineSpec{}
	spec.AddElement("src", "fakesrc", nil)
	spec.Link("src", "nonexistent")

	pipeline, err := exec.Build(spec)
	assert.Error(t, err)
	assert.Nil(t, pipeline)
	assert.Contains(t, err.Error(), "unknown element")
}

func TestExecutor_BuildSetsProperties(t *testing.T) {
	log := zerolog.Nop()
	exec := NewExecutor(log)

	spec := PipelineSpec{}
	spec.AddElement("src", "fakesrc", Props{
		"num-buffers": 42,
	})
	spec.AddElement("sink", "fakesink", Props{
		"sync": false,
	})
	spec.Link("src", "sink")

	pipeline, err := exec.Build(spec)
	require.NoError(t, err)
	defer exec.Stop(pipeline)

	src, srcErr := pipeline.GetElementByName("src")
	require.NoError(t, srcErr)
	require.NotNil(t, src)

	val, propErr := src.GetProperty("num-buffers")
	require.NoError(t, propErr)
	assert.Equal(t, 42, val)
}

func TestExecutor_RunProducesResult(t *testing.T) {
	log := zerolog.Nop()
	exec := NewExecutor(log)

	spec := PipelineSpec{}
	spec.AddElement("src", "fakesrc", Props{"num-buffers": 0})
	spec.AddElement("sink", "fakesink", Props{"sync": false})
	spec.Link("src", "sink")

	pipeline, result := exec.Run(nil, spec)
	if pipeline != nil {
		exec.Stop(pipeline)
	}
	if result.Err != nil {
		assert.NotEmpty(t, result.ExitReason)
	}
}

func TestExecutor_StopNilPipeline(t *testing.T) {
	log := zerolog.Nop()
	exec := NewExecutor(log)
	exec.Stop(nil)
}
