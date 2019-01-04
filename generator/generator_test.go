package generator

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestGenerator_GetTime(t *testing.T) {
	generator := New(1)
	expect := "2019-01-04T11:06:08.28+08:00"
	Convey("GetTime func should return format time string", t, func() {
		time := generator.GetTime(530703474355077120)
		So(time, ShouldEqual, expect)
	})
}
