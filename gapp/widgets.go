package main

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// newTheme returns a dark material theme close to the Fyne build's
// default look.
func newTheme() *material.Theme {
	th := material.NewTheme()
	th.Palette.Bg = color.NRGBA{R: 0x1e, G: 0x1e, B: 0x24, A: 0xff}
	th.Palette.Fg = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xec, A: 0xff}
	th.Palette.ContrastBg = color.NRGBA{R: 0x2f, G: 0x6f, B: 0xed, A: 0xff}
	th.Palette.ContrastFg = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	th.TextSize = unit.Sp(13.5)
	return th
}

var (
	colRowAlt  = color.NRGBA{R: 0x26, G: 0x26, B: 0x2e, A: 0xff}
	colRowSel  = color.NRGBA{R: 0x2c, G: 0x3e, B: 0x63, A: 0xff}
	colDivider = color.NRGBA{R: 0x44, G: 0x44, B: 0x52, A: 0xff}
	colDim     = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x99}
	colCard    = color.NRGBA{R: 0x28, G: 0x28, B: 0x32, A: 0xff}
)

// fillRect paints a solid rectangle over the given size.
func fillRect(gtx layout.Context, size image.Point, c color.NRGBA) {
	paint.FillShape(gtx.Ops, c, clip.Rect{Max: size}.Op())
}

// bgWidget returns a layout.Widget that fills its whole area with c
// (used as the Expanded layer of a Stack behind row content).
func bgWidget(c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		sz := gtx.Constraints.Min
		fillRect(gtx, sz, c)
		return layout.Dimensions{Size: sz}
	}
}

// cellLabel renders one table cell: fixed dp width, single line,
// clipped. bold selects the bold face.
func cellLabel(th *material.Theme, width unit.Dp, text string, bold bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		w := gtx.Dp(width)
		gtx.Constraints.Min.X = w
		gtx.Constraints.Max.X = w
		defer clip.Rect{Max: image.Pt(w, gtx.Constraints.Max.Y)}.Push(gtx.Ops).Pop()
		l := material.Label(th, th.TextSize, text)
		l.MaxLines = 1
		if bold {
			l.Font.Weight = font.Bold
		}
		return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
}

// choice is a minimal drop-down: a button showing the current label;
// while open, the options render inline below it (the pages scroll,
// so an expanding list is the simplest robust shape).
type choice struct {
	opts     []valueLabel
	sel      int
	open     bool
	btn      widget.Clickable
	optClick []widget.Clickable
	onChange func(value string)
}

func newChoice(opts []valueLabel, value string, onChange func(string)) *choice {
	c := &choice{opts: opts, onChange: onChange}
	c.sel = indexOfOption(opts, value)
	c.optClick = make([]widget.Clickable, len(opts))
	return c
}

func (c *choice) value() string { return c.opts[c.sel].value }
func (c *choice) label() string { return c.opts[c.sel].label }

// setValue moves the selection without firing onChange.
func (c *choice) setValue(v string) {
	c.sel = indexOfOption(c.opts, v)
}

func (c *choice) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if c.btn.Clicked(gtx) {
		c.open = !c.open
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, &c.btn, c.label()+"  ▾").Layout(gtx)
		}),
	}
	if c.open {
		for i := range c.opts {
			i := i
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if c.optClick[i].Clicked(gtx) {
					c.sel = i
					c.open = false
					if c.onChange != nil {
						c.onChange(c.opts[i].value)
					}
				}
				btn := material.Button(th, &c.optClick[i], c.opts[i].label)
				if i != c.sel {
					btn.Background = th.Bg
					btn.Color = th.Fg
				}
				return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, btn.Layout)
			}))
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// ---------- modal framework ----------

// layoutModal draws the dim backdrop + centered card for a.modal.
func (a *App) layoutModal(gtx layout.Context) layout.Dimensions {
	// Swallow clicks outside the card.
	_ = a.modalBg.Clicked(gtx)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(bgWidget(colDim)),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				maxW := gtx.Dp(unit.Dp(660))
				if gtx.Constraints.Max.X > maxW {
					gtx.Constraints.Max.X = maxW
				}
				maxH := gtx.Dp(unit.Dp(640))
				if gtx.Constraints.Max.Y > maxH {
					gtx.Constraints.Max.Y = maxH
				}
				return widget.Border{
					Color:        colDivider,
					CornerRadius: unit.Dp(8),
					Width:        unit.Dp(1),
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(bgWidget(colCard)),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{
								Top: unit.Dp(14), Bottom: unit.Dp(14),
								Left: unit.Dp(16), Right: unit.Dp(16),
							}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return a.modal.layoutModal(gtx, a)
							})
						}),
					)
				})
			})
		}),
	)
}

// modalTitle renders a bold modal heading.
func modalTitle(th *material.Theme, text string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(15), text)
		l.Font.Weight = font.Bold
		return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, l.Layout)
	}
}

// ---------- confirm / error modals ----------

type confirmModal struct {
	title, msg string
	onOK       func()
	okBtn      widget.Clickable
	cancelBtn  widget.Clickable
}

func (a *App) showConfirm(title, msg string, onOK func()) {
	a.modal = &confirmModal{title: title, msg: msg, onOK: onOK}
}

func (m *confirmModal) layoutModal(gtx layout.Context, a *App) layout.Dimensions {
	if m.cancelBtn.Clicked(gtx) {
		a.modal = nil
		return layout.Dimensions{}
	}
	if m.okBtn.Clicked(gtx) {
		a.modal = nil
		if m.onOK != nil {
			m.onOK()
		}
		return layout.Dimensions{}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(modalTitle(a.th, m.title)),
		layout.Rigid(material.Label(a.th, a.th.TextSize, m.msg).Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(material.Button(a.th, &m.cancelBtn, "Cancel").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(a.th, &m.okBtn, "OK").Layout),
				)
			})
		}),
	)
}

type errorModal struct {
	msg   string
	okBtn widget.Clickable
}

func (a *App) showError(err error) {
	a.modal = &errorModal{msg: err.Error()}
}

func (m *errorModal) layoutModal(gtx layout.Context, a *App) layout.Dimensions {
	if m.okBtn.Clicked(gtx) {
		a.modal = nil
		return layout.Dimensions{}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(modalTitle(a.th, "Error")),
		layout.Rigid(material.Label(a.th, a.th.TextSize, m.msg).Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(material.Button(a.th, &m.okBtn, "OK").Layout),
				)
			})
		}),
	)
}

// formRow renders one "label + editor" line of a modal form.
func formRow(th *material.Theme, label string, ed *widget.Editor, hint string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(170))
					return material.Label(th, th.TextSize, label).Layout(gtx)
				}),
				layout.Flexed(1, material.Editor(th, ed, hint).Layout),
			)
		})
	})
}
