package aircountv1

import _ "embed"

//go:embed detection.proto
var DetectionProto string

//go:embed csi.proto
var CSIProto string
