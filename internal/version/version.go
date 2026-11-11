package version

// Version is the panel/agent release string. Install and hello frames report it.
const Version = "0.3.34"

// VersionMarkerPrefix is searched for in the agent file. A bare version is not
// enough: the linker glues adjacent rodata, so v0.3.0's only copy of "0.3.0"
// was the tail of "sse20.3.0". The boundary check then rejected the real binary
// and one-click upgrade refused to send it.
const VersionMarkerPrefix = "liking-version="

// VersionMarker is one string literal. A const concatenation can be split, and
// then the agent file would not contain the bytes the panel looks for.
const VersionMarker = "liking-version=0.3.34"

// linkedMarker is read at init so the linker keeps VersionMarker in every
// binary that imports this package. A const-only use can be folded away.
var linkedMarker = VersionMarker

func init() {
	if VersionMarker != VersionMarkerPrefix+Version {
		panic("version marker drifted from Version")
	}
	if len(linkedMarker) == 0 {
		panic("empty version marker")
	}
}
