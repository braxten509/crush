// Package anim provides an animated spinner.
package anim

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zeebo/xxh3"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/charmbracelet/crush/internal/csync"
)

const (
	fps           = 20
	initialChar   = '.'
	labelGap      = " "
	labelGapWidth = 1

	// Periods of ellipsis animation speed in steps.
	//
	// If the FPS is 20 (50 milliseconds) this means that the ellipsis will
	// change every 8 frames (400 milliseconds).
	ellipsisAnimSpeed = 8

	// The maximum number of animation steps that can pass before a
	// character appears. With fps == 20 this is ~1s of staggered
	// entrance, identical to the previous wall-clock-driven value.
	// Switching from wall-clock + rand to a step-driven birth schedule
	// keeps Render() deterministic: two Anim instances built from the
	// same Settings produce byte-identical output when no Animate ticks
	// have advanced their step counter.
	maxBirthSteps = 20

	// Number of frames to prerender for the animation. After this number
	// of frames, the animation will loop. This only applies when color
	// cycling is disabled.
	prerenderedFrames = 10

	// Default number of cycling chars.
	defaultNumCyclingChars = 10
)

// Default colors for gradient.
var (
	defaultGradColorA = color.RGBA{R: 0xff, G: 0, B: 0, A: 0xff}
	defaultGradColorB = color.RGBA{R: 0, G: 0, B: 0xff, A: 0xff}
)

var (
	availableRunes = []rune("0123456789abcdefABCDEF~!@#$£€%^&*()+=_")
	// Reserve three columns so the timer stays fixed across frames.
	ellipsisFrames = []string{".  ", ".. ", "..."}
)

// Internal ID management. The ID seeds the deterministic birth schedule so
// two spinners built from the same settings do not animate in lockstep.
var lastID atomic.Int64

func nextID() int {
	return int(lastID.Add(1))
}

// Cache for expensive animation calculations
type animCache struct {
	initialFrames  [][]string
	cyclingFrames  [][]string
	width          int
	labelWidth     int
	label          []string
	ellipsisFrames []string
}

var animCacheMap = csync.NewMap[string, *animCache]()

// settingsHash creates a hash key for the settings to use for caching
func settingsHash(opts Settings) string {
	h := xxh3.New()
	fmt.Fprintf(h, "%d-%s-%v-%v-%v-%t-%v",
		opts.Size, opts.Label, opts.LabelColor, opts.GradColorA, opts.GradColorB, opts.CycleColors, opts.SuffixColor)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// FrameInterval returns how long each animation frame stays on screen. The
// UI drives every Anim from one clock ticking at this interval; Anim
// instances never schedule themselves.
func FrameInterval() time.Duration {
	return time.Second / time.Duration(fps)
}

// Settings defines settings for the animation.
type Settings struct {
	ID          string
	Size        int
	Label       string
	LabelColor  color.Color
	GradColorA  color.Color
	GradColorB  color.Color
	CycleColors bool

	// NoScramble disables the scrambled rune animation. The cycling
	// character region is removed entirely so only the label and its
	// animated ellipsis are visible. Useful for non-LLM contexts where
	// scrambled glyphs imply "thinking" rather than "running".
	NoScramble bool

	// Suffix is an optional function that returns a dynamic suffix string
	// to render after the label and ellipsis. Called on every Render().
	Suffix func() string

	// SuffixColor is the color used to render the suffix text.
	// Falls back to LabelColor if unset.
	SuffixColor color.Color
}

// Default settings.
const ()

// Anim is a Bubble for an animated spinner.
type Anim struct {
	width            int
	cyclingCharWidth int
	label            *csync.Slice[string]
	labelWidth       int
	labelColor       color.Color
	birthSteps       []int
	initialFrames    [][]string // frames for the initial characters
	initialized      atomic.Bool
	cyclingFrames    [][]string           // frames for the cycling characters
	step             atomic.Int64         // current main frame step (wraps)
	framesSinceStart atomic.Int64         // total Advance frames (does not wrap)
	ellipsisStep     atomic.Int64         // current ellipsis frame step
	ellipsisFrames   *csync.Slice[string] // ellipsis animation frames
	id               string
	suffix           func() string
	suffixColor      color.Color
	settings         Settings
}

// New creates a new Anim instance with the specified width and label.
func New(opts Settings) *Anim {
	a := &Anim{}
	// Validate settings.
	if opts.Size < 1 {
		opts.Size = defaultNumCyclingChars
	}
	if colorIsUnset(opts.GradColorA) {
		opts.GradColorA = defaultGradColorA
	}
	if colorIsUnset(opts.GradColorB) {
		opts.GradColorB = defaultGradColorB
	}
	// A nil LabelColor means "use the terminal default foreground".
	// No fallback is applied so non-interactive callers can opt out
	// of explicit coloring.

	if opts.ID != "" {
		a.id = opts.ID
	} else {
		a.id = fmt.Sprintf("%d", nextID())
	}
	if opts.NoScramble {
		a.cyclingCharWidth = 0
	} else {
		a.cyclingCharWidth = opts.Size
	}
	a.labelColor = opts.LabelColor

	// Store the suffix function if provided.
	if opts.Suffix != nil {
		a.suffix = opts.Suffix
	}
	if opts.SuffixColor != nil {
		a.suffixColor = opts.SuffixColor
	} else {
		a.suffixColor = opts.LabelColor
	}

	// NoScramble means no cycling chars and no birth animation. Mark as
	// initialized immediately so the label renders without a fade-in.
	if opts.NoScramble {
		a.initialized.Store(true)
	}

	a.settings = opts
	a.buildFrames()

	return a
}

// buildFrames generates or fetches from the cache all pre-rendered
// animation frames for the current settings. Calling it again after
// SetColors rebuilds the frames while the step counters keep advancing,
// so a theme swap mid-animation does not restart the spinner.
func (a *Anim) buildFrames() {
	opts := a.settings

	// Check cache first
	cacheKey := settingsHash(opts)
	if cached, exists := animCacheMap.Get(cacheKey); exists {
		a.useCache(cached)
	} else {
		a.generateFrames(opts, cacheKey)
	}
	a.scheduleBirths(cacheKey)
}

// useCache adopts frames built earlier for the same settings.
func (a *Anim) useCache(cached *animCache) {
	a.width = cached.width
	a.labelWidth = cached.labelWidth
	a.label = csync.NewSliceFrom(cached.label)
	a.ellipsisFrames = csync.NewSliceFrom(cached.ellipsisFrames)
	a.initialFrames = cached.initialFrames
	a.cyclingFrames = cached.cyclingFrames
}

// generateFrames measures the animation, pre-renders its label and frames,
// and caches the result under cacheKey.
func (a *Anim) generateFrames(opts Settings, cacheKey string) {
	a.labelWidth = lipgloss.Width(opts.Label)

	// Total width of anim, in cells. When NoScramble is set there
	// are no cycling chars so the label gap is unnecessary.
	a.width = a.cyclingCharWidth
	if opts.Label != "" {
		if a.cyclingCharWidth > 0 {
			a.width += labelGapWidth
		}
		a.width += lipgloss.Width(opts.Label)
	}

	// Render the label
	a.renderLabel(opts.Label)

	// Pre-generate gradient.
	var ramp []color.Color
	numFrames := prerenderedFrames
	if opts.CycleColors {
		ramp = makeGradientRamp(a.width*3, opts.GradColorA, opts.GradColorB, opts.GradColorA, opts.GradColorB)
		numFrames = a.width * 2
	} else {
		ramp = makeGradientRamp(a.width, opts.GradColorA, opts.GradColorB)
	}

	a.initialFrames = a.renderInitialFrames(opts, ramp, numFrames)
	a.cyclingFrames = a.renderCyclingFrames(opts, ramp, numFrames, xxh3.HashString(cacheKey))

	// Cache the results
	labelSlice := make([]string, a.label.Len())
	for i, v := range a.label.Seq2() {
		labelSlice[i] = v
	}
	ellipsisSlice := make([]string, a.ellipsisFrames.Len())
	for i, v := range a.ellipsisFrames.Seq2() {
		ellipsisSlice[i] = v
	}
	animCacheMap.Set(cacheKey, &animCache{
		initialFrames:  a.initialFrames,
		cyclingFrames:  a.cyclingFrames,
		width:          a.width,
		labelWidth:     a.labelWidth,
		label:          labelSlice,
		ellipsisFrames: ellipsisSlice,
	})
}

// renderInitialFrames pre-renders the initial character for every column,
// colored along the gradient ramp (and shifted per frame when cycling
// colors), so the render loop does no styling.
func (a *Anim) renderInitialFrames(opts Settings, ramp []color.Color, numFrames int) [][]string {
	frames := make([][]string, numFrames)
	offset := 0
	for i := range frames {
		frames[i] = make([]string, a.width+labelGapWidth+a.labelWidth)
		for j := range frames[i] {
			if j+offset >= len(ramp) {
				continue // skip if we run out of colors
			}

			var c color.Color
			if j <= a.cyclingCharWidth {
				c = ramp[j+offset]
			} else {
				c = opts.LabelColor
			}

			// Also prerender the initial character with Lip Gloss to avoid
			// processing in the render loop.
			frames[i][j] = lipgloss.NewStyle().
				Foreground(c).
				Render(string(initialChar))
		}
		if opts.CycleColors {
			offset++
		}
	}
	return frames
}

// renderCyclingFrames prerenders scrambled rune frames for the animation.
// The rune picker is seeded off the settings hash so the frames are a pure
// function of Settings: two processes with identical Settings populate the
// cache with the same glyphs, which keeps any cross-process golden-file
// comparison stable.
func (a *Anim) renderCyclingFrames(opts Settings, ramp []color.Color, numFrames int, seed uint64) [][]string {
	rng := rand.New(rand.NewPCG(seed, ^seed))
	frames := make([][]string, numFrames)
	offset := 0
	for i := range frames {
		frames[i] = make([]string, a.width)
		for j := range frames[i] {
			if j+offset >= len(ramp) {
				continue // skip if we run out of colors
			}

			// Also prerender the color with Lip Gloss here to avoid processing
			// in the render loop.
			r := availableRunes[rng.IntN(len(availableRunes))]
			frames[i][j] = lipgloss.NewStyle().
				Foreground(ramp[j+offset]).
				Render(string(r))
		}
		if opts.CycleColors {
			offset++
		}
	}
	return frames
}

// scheduleBirths assigns a deterministic birth step to each column for a
// staggered entrance effect. The schedule is seeded off the spinner id and
// the settings hash, so two spinners with the same role and identity
// stagger identically (this is what keeps Render() byte-equal across cache
// hits and across processes for the same Settings+ID) while spinners with
// different ids — distinct assistant messages, different tool calls,
// "Thinking" vs "Generating" labels — fade in with different patterns
// instead of marching in lock-step. A rebuild only for new colors keeps the
// existing schedule so an in-flight fade-in is not disturbed.
func (a *Anim) scheduleBirths(cacheKey string) {
	if len(a.birthSteps) == a.width {
		return
	}
	birthSeed := xxh3.HashString(a.id + "|" + cacheKey)
	birthRng := rand.New(rand.NewPCG(birthSeed, ^birthSeed))
	a.birthSteps = make([]int, a.width)
	for i := range a.birthSteps {
		a.birthSteps[i] = birthRng.IntN(maxBirthSteps)
	}
}

// SetLabel updates the label text and re-renders it.
func (a *Anim) SetLabel(newLabel string) {
	a.settings.Label = newLabel
	a.labelWidth = lipgloss.Width(newLabel)

	// Update total width. Skip the label gap when there are no cycling chars.
	a.width = a.cyclingCharWidth
	if newLabel != "" {
		if a.cyclingCharWidth > 0 {
			a.width += labelGapWidth
		}
		a.width += a.labelWidth
	}

	// Re-render the label
	a.renderLabel(newLabel)
}

// SetColors updates the animation's colors and rebuilds its pre-rendered
// frames. It is used when the theme changes while a spinner is live, so
// the animation picks up the new gradient, label, and suffix colors
// without restarting mid-flight. A nil suffixColor falls back to the
// label color; unset gradient colors fall back to the defaults.
func (a *Anim) SetColors(labelColor, gradColorA, gradColorB, suffixColor color.Color) {
	opts := a.settings
	opts.LabelColor = labelColor
	opts.GradColorA = gradColorA
	opts.GradColorB = gradColorB
	opts.SuffixColor = suffixColor
	if colorIsUnset(opts.GradColorA) {
		opts.GradColorA = defaultGradColorA
	}
	if colorIsUnset(opts.GradColorB) {
		opts.GradColorB = defaultGradColorB
	}
	a.settings = opts
	a.labelColor = opts.LabelColor
	if opts.SuffixColor != nil {
		a.suffixColor = opts.SuffixColor
	} else {
		a.suffixColor = opts.LabelColor
	}
	a.buildFrames()
}

// renderLabel renders the label with the current label color.
func (a *Anim) renderLabel(label string) {
	if a.labelWidth > 0 {
		// Pre-render the label.
		labelRunes := []rune(label)
		a.label = csync.NewSlice[string]()
		for i := range labelRunes {
			rendered := lipgloss.NewStyle().
				Foreground(a.labelColor).
				Render(string(labelRunes[i]))
			a.label.Append(rendered)
		}

		// Pre-render the ellipsis frames which come after the label.
		a.ellipsisFrames = csync.NewSlice[string]()
		for _, frame := range ellipsisFrames {
			rendered := lipgloss.NewStyle().
				Foreground(a.labelColor).
				Render(frame)
			a.ellipsisFrames.Append(rendered)
		}
	} else {
		a.label = csync.NewSlice[string]()
		a.ellipsisFrames = csync.NewSlice[string]()
	}
}

// Width returns the total width of the animation.
func (a *Anim) Width() (w int) {
	w = a.width
	if a.labelWidth > 0 {
		w += labelGapWidth + a.labelWidth

		var widestEllipsisFrame int
		for _, f := range ellipsisFrames {
			fw := lipgloss.Width(f)
			if fw > widestEllipsisFrame {
				widestEllipsisFrame = fw
			}
		}
		w += widestEllipsisFrame
	}
	return w
}

// Advance moves the animation forward by one frame. It is called by the
// UI's shared animation clock for every visible spinner; the Anim itself
// never schedules ticks.
func (a *Anim) Advance() bool {
	step := a.step.Add(1)
	if int(step) >= len(a.cyclingFrames) {
		a.step.Store(0)
	}

	frames := a.framesSinceStart.Add(1)
	if a.initialized.Load() && a.labelWidth > 0 {
		// Manage the ellipsis animation.
		ellipsisStep := a.ellipsisStep.Add(1)
		if int(ellipsisStep) >= ellipsisAnimSpeed*len(ellipsisFrames) {
			a.ellipsisStep.Store(0)
		}
	} else if !a.initialized.Load() && int(frames) >= maxBirthSteps {
		a.initialized.Store(true)
	}
	return true
}

// Render renders the current state of the animation.
func (a *Anim) Render() string {
	var b strings.Builder
	step := int(a.step.Load())
	frames := int(a.framesSinceStart.Load())
	for i := range a.width {
		switch {
		case !a.initialized.Load() && i < len(a.birthSteps) && frames < a.birthSteps[i]:
			// Birth step not reached: render initial character.
			b.WriteString(a.initialFrames[step][i])
		case i < a.cyclingCharWidth:
			// Render a cycling character.
			b.WriteString(a.cyclingFrames[step][i])
		case i == a.cyclingCharWidth && a.cyclingCharWidth > 0:
			// Render label gap (only when there are cycling chars).
			b.WriteString(labelGap)
		default:
			// Label. Offset past cycling chars and gap (if any).
			offset := a.cyclingCharWidth
			if a.cyclingCharWidth > 0 {
				offset += labelGapWidth
			}
			if labelChar, ok := a.label.Get(i - offset); ok {
				b.WriteString(labelChar)
			}
		}
	}
	// Keep the animated ellipsis visible alongside the elapsed time.
	if a.initialized.Load() && a.labelWidth > 0 {
		b.WriteString(a.RenderEllipsis())
	}

	// Render optional suffix (e.g., elapsed time).
	if a.suffix != nil {
		suffixStr := a.suffix()
		if suffixStr != "" {
			b.WriteString(" ")
			b.WriteString(lipgloss.NewStyle().Foreground(a.suffixColor).Render(suffixStr))
		}
	}

	return b.String()
}

// RenderEllipsis returns the current animated dots without scrambled glyphs.
func (a *Anim) RenderEllipsis() string {
	step := int(a.ellipsisStep.Load()) / ellipsisAnimSpeed % len(ellipsisFrames)
	return lipgloss.NewStyle().Foreground(a.labelColor).Render(ellipsisFrames[step])
}

// makeGradientRamp() returns a slice of colors blended between the given keys.
// Blending is done as Hcl to stay in gamut.
func makeGradientRamp(size int, stops ...color.Color) []color.Color {
	if len(stops) < 2 {
		return nil
	}

	points := make([]colorful.Color, len(stops))
	for i, k := range stops {
		points[i], _ = colorful.MakeColor(k)
	}

	numSegments := len(stops) - 1
	if numSegments == 0 {
		return nil
	}
	blended := make([]color.Color, 0, size)

	// Calculate how many colors each segment should have.
	segmentSizes := make([]int, numSegments)
	baseSize := size / numSegments
	remainder := size % numSegments

	// Distribute the remainder across segments.
	for i := range numSegments {
		segmentSizes[i] = baseSize
		if i < remainder {
			segmentSizes[i]++
		}
	}

	// Generate colors for each segment.
	for i := range numSegments {
		c1 := points[i]
		c2 := points[i+1]
		segmentSize := segmentSizes[i]

		for j := range segmentSize {
			if segmentSize == 0 {
				continue
			}
			t := float64(j) / float64(segmentSize)
			c := c1.BlendHcl(c2, t)
			blended = append(blended, c)
		}
	}

	return blended
}

func colorIsUnset(c color.Color) bool {
	if c == nil {
		return true
	}
	_, _, _, a := c.RGBA()
	return a == 0
}
