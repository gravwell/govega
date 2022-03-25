package govega

import (
	"github.com/tfriedel6/canvas"
	"github.com/tfriedel6/canvas/backend/softwarebackend"
)

type mycanvas struct {
	*canvas.Canvas

	// Thought about
	GlobalAlpha float64
	StrokeStyle string

	// Pasted, good luck
	CurrentTransform         string
	Direction                string
	FillStyle                string
	Filter                   string
	Font                     string
	GlobalCompositeOperation string
	ImageSmoothingEnabled    string
	ImageSmoothingQuality    string
	LineCap                  string
	LineDashOffset           string
	LineJoin                 string
	LineWidth                string
	MiterLimit               string
	ShadowBlur               string
	ShadowColor              string
	ShadowOffsetX            string
	ShadowOffsetY            string
	TextAlign                string
	TextBaseline             string
}

func mkCanvas() *mycanvas {
	s := softwarebackend.New(500, 500)
	c := canvas.New(s)

	font, _ := c.LoadFont("/home/michaelwisely/code/govega/bin/govega/MigantyScript.ttf")
	c.SetFont(font, 16)

	return &mycanvas{
		Canvas: c,
	}
}
