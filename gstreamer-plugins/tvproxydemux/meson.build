project('gst-tvproxydemux', 'c', version: '0.1.0')

gst_dep = dependency('gstreamer-1.0', version: '>= 1.20')
gst_base_dep = dependency('gstreamer-base-1.0', version: '>= 1.20')

library('gsttvproxydemux',
  'src/plugin.c',
  'src/gsttvproxydemux.c',
  c_args: ['-DPACKAGE="gsttvproxydemux"'],
  dependencies: [gst_dep, gst_base_dep],
  install: false,
  name_prefix: '',
)
