package govega

import (
	"embed"
	"fmt"

	"github.com/dop251/goja"
	"github.com/tfriedel6/canvas"
	"github.com/tfriedel6/canvas/backend/softwarebackend"
)

//go:embed "fonts/Roboto-Regular.ttf"
var fonts embed.FS

// The first font in the list will be the default font
var fontFiles = []string{
	"fonts/Roboto-Regular.ttf",
}

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

func mkCanvas(w, h int) (*mycanvas, error) {
	s := softwarebackend.New(w, h)
	c := canvas.New(s)

	for _, s := range fontFiles {
		fontBytes, err := fonts.ReadFile(s)
		if err != nil {
			return nil, fmt.Errorf("Failed to open embedded font file %q %w", s, err)
		}

		if _, err := c.LoadFont(fontBytes); err != nil {
			return nil, fmt.Errorf("Failed to parse font %q %w", s, err)
		}

	}

	// The font that's loaded first is set as the default font.
	// Passing nil indicates we want to use the default
	c.SetFont(nil, 12)

	//init the fill style to be a white background
	//if we don't do this its possible to panic by calling fill without setting the fill style
	c.SetFillStyle("#FFFFFF")

	return &mycanvas{Canvas: c}, nil
}

func initCanvas(vm *goja.Runtime, c *mycanvas) (err error) {
	v := vm.ToValue(c)
	if v == nil {
		return fmt.Errorf("failed to create value out of canvas")
	}
	obj := v.ToObject(vm)
	if obj == nil {
		return fmt.Errorf("failed to create object out of value")
	}

	//TODO go gin up the overrides for getters and setters where appropriate

	if err = vm.Set("cxt", obj); err != nil {
		err = fmt.Errorf("Failed to set cxt object %w", err)
	}
	return
}
