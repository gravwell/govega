package govega

import (
	"embed"
	"fmt"
	"strconv"
	"strings"

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

	// custom getter/setter
	fillStyle     string
	strokeStyle   string
	shadowColor   string
	shadowBlur    float64
	shadowOffsetX float64
	shadowOffsetY float64
	globalAlpha   float64
	font          string
	fontSize      float64
	textAlign     string
	textBaseline  string //partially implemented

	lineCap        string
	lineJoin       string
	lineWidth      float64
	miterLimit     float64
	lineDashOffset float64

	// maybe unimplmented
	data   string
	width  int
	height int

	// Canvas missing features
	GlobalCompositeOperation string
	ImageSmoothingEnabled    string
	ImageSmoothingQuality    string
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

	return &mycanvas{
		Canvas:    c,
		fontSize:  12,
		fillStyle: "#FFFFFF",
		width:     w,
		height:    h,
	}, nil
}

func initCanvas(vm *goja.Runtime, c *mycanvas) (err error) {
	v := vm.ToValue(c)
	if v == nil {
		return fmt.Errorf("failed to create value out of canvas")
	}
	bobj := v.ToObject(vm)
	if bobj == nil {
		return fmt.Errorf("failed to create object out of value")
	}
	obj := vm.NewObject()
	if err = obj.SetPrototype(bobj); err != nil {
		return fmt.Errorf("failed to set prototype object %w", err)
	}

	// FillStyle
	if err = obj.DefineAccessorProperty("fillStyle",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.fillStyle)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.fillStyle = call.Argument(0).String()
			c.SetFillStyle(c.fillStyle)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set fillStyle accessors %w", err)
		return
	}

	// StrokeStyle
	if err = obj.DefineAccessorProperty("strokeStyle",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.strokeStyle)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.strokeStyle = call.Argument(0).String()
			c.SetStrokeStyle(c.strokeStyle)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set strokeStyle accessors %w", err)
		return
	}

	//GlobalAlpha
	if err = obj.DefineAccessorProperty("globalAlpha",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.globalAlpha)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.globalAlpha = call.Argument(0).ToFloat()
			c.SetGlobalAlpha(c.globalAlpha)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set globalAlpha accessors %w", err)
		return
	}

	//ShadowColor
	if err = obj.DefineAccessorProperty("shadowColor",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.shadowColor)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.shadowColor = call.Argument(0).String()
			c.SetShadowColor(c.shadowColor)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set globalAlpha accessors %w", err)
		return
	}

	//ShadowBlur
	if err = obj.DefineAccessorProperty("shadowBlur",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.shadowBlur)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.shadowBlur = call.Argument(0).ToFloat()
			c.SetShadowBlur(c.shadowBlur)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set globalAlpha accessors %w", err)
		return
	}

	//ShadowOffsetX
	if err = obj.DefineAccessorProperty("shadowOffsetX",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.shadowOffsetX)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.shadowOffsetX = call.Argument(0).ToFloat()
			c.SetShadowOffsetX(c.shadowOffsetX)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set globalAlpha accessors %w", err)
		return
	}

	//ShadowOffsetY
	if err = obj.DefineAccessorProperty("shadowOffsetY",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.shadowOffsetY)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.shadowOffsetY = call.Argument(0).ToFloat()
			c.SetShadowOffsetY(c.shadowOffsetY)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set globalAlpha accessors %w", err)
		return
	}

	//Font
	if err = obj.DefineAccessorProperty("font",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.font)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			name, size := parseFont(call.Argument(0).String())
			if size == 0 {
				size = c.fontSize
			}
			if name == `` {
				name = c.font
			}
			c.font = name
			c.fontSize = size
			c.SetFontByName(c.font, c.fontSize)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set font accessors %w", err)
		return
	}

	//TextAlign
	if err = obj.DefineAccessorProperty("textAlign",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.textAlign)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.textAlign = call.Argument(0).String()
			c.SetTextAlign(canvas.ParseTextAlign(c.textAlign))
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set textAlign accessors %w", err)
		return
	}

	//TextBaseline
	if err = obj.DefineAccessorProperty("textBaseline",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.textBaseline)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.textBaseline = call.Argument(0).String()
			c.SetTextBaseline(canvas.ParseTextBaseline(c.textBaseline))
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set textBaseline accessors %w", err)
		return
	}

	//LineCap
	if err = obj.DefineAccessorProperty("lineCap",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.lineCap)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.lineCap = call.Argument(0).String()
			c.SetLineCap(canvas.ParseLineCap(c.lineCap))
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set lineCap accessors %w", err)
		return
	}

	//LineJoin
	if err = obj.DefineAccessorProperty("lineJoin",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.lineJoin)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.lineJoin = call.Argument(0).String()
			c.SetLineJoin(canvas.ParseLineJoin(c.lineJoin))
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set lineJoin accessors %w", err)
		return
	}

	//LineWidth
	if err = obj.DefineAccessorProperty("lineWidth",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.lineWidth)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.lineWidth = call.Argument(0).ToFloat()
			c.SetLineWidth(c.lineWidth)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set lineWidth accessors %w", err)
		return
	}

	//miterLimit
	if err = obj.DefineAccessorProperty("miterLimit",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.miterLimit)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.miterLimit = call.Argument(0).ToFloat()
			c.SetMiterLimit(c.miterLimit)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set miterLimit accessors %w", err)
		return
	}

	//LineDashOffset
	if err = obj.DefineAccessorProperty("lineDashOffset",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.lineDashOffset)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.lineDashOffset = call.Argument(0).ToFloat()
			c.SetLineDashOffset(c.lineDashOffset)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set lineDashOffset accessors %w", err)
		return
	}

	//Width
	if err = obj.DefineAccessorProperty("width",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.width)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set width accessors %w", err)
		return
	}

	//Height
	if err = obj.DefineAccessorProperty("height",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(c.height)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set height accessors %w", err)
		return
	}

	//Data
	if err = obj.DefineAccessorProperty("data",
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			fmt.Println("Returned data", c.data)
			return vm.ToValue(c.data)
		}),
		vm.ToValue(func(call goja.FunctionCall) (ret goja.Value) {
			c.data = call.Argument(0).String()
			fmt.Println("Set Data", c.data)
			return
		}),
		goja.FLAG_TRUE, goja.FLAG_TRUE,
	); err != nil {
		err = fmt.Errorf("failed to set lineDashOffset accessors %w", err)
		return
	}

	//TODO go gin up the overrides for getters and setters where appropriate

	if err = vm.Set("cxt", obj); err != nil {
		err = fmt.Errorf("Failed to set cxt object %w", err)
	}
	return
}

func parseFont(v string) (name string, size float64) {
	flds := strings.Fields(v)
	if len(flds) == 2 {
		if strings.HasSuffix(flds[0], `px`) {
			size, _ = strconv.ParseFloat(strings.TrimSuffix(flds[0], `px`), 64)
		}
		name = flds[1]
	}
	//translate the fonts to what we actually have
	switch name {
	case `sans-serif`:
		name = `Noto Sans Regular`
	}
	return
}
