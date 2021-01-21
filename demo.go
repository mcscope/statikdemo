package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"math/rand"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"golang.org/x/exp/shiny/driver"
	"golang.org/x/exp/shiny/screen"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/mouse"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
)

const (
	// N = 128 // The grid of cells has size NxN.
	N = 200 // The grid of cells has size NxN.

	tickDuration  = time.Second / 40
	stageDuration = time.Second * 25
	MAX_STAGE     = 6
	INIT_STAGE    = 0
)

const (
	UNFROZEN    byte = iota
	NEAR_FROZEN byte = iota
	FROZEN      byte = iota
)

func main() {
	driver.Main(func(s screen.Screen) {
		f, err := os.Create("profile")
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()

		w, err := s.NewWindow(&screen.NewWindowOptions{
			Title:  "LadyRed's Statiktine",
			Width:  N * 4,
			Height: N * 4,
		})
		if err != nil {
			log.Fatal(err)
		}
		buf, tex := screen.Buffer(nil), screen.Texture(nil)
		defer func() {
			if buf != nil {
				tex.Release()
				buf.Release()
			}
			w.Release()
		}()

		go run_scenes(w)

		var (
			buttonDown bool
			sz         size.Event
		)
		for {
			publish := false
			next_event := w.NextEvent()
			switch e := next_event.(type) {
			case lifecycle.Event:
				if e.To == lifecycle.StageDead {
					return
				}
				switch e.Crosses(lifecycle.StageVisible) {
				case lifecycle.CrossOn:
					// TODO pretty sure this doesn't work
					pauseChan <- play
					var err error
					buf, err = s.NewBuffer(image.Point{N, N})
					if err != nil {
						log.Fatal(err)
					}
					tex, err = s.NewTexture(image.Point{N, N})
					if err != nil {
						log.Fatal(err)
					}
					tex.Fill(tex.Bounds(), color.White, draw.Src)

				case lifecycle.CrossOff:
					pauseChan <- pause
					tex.Release()
					tex = nil
					buf.Release()
					buf = nil
				}

			case mouse.Event:
				if e.Button == mouse.ButtonLeft {
					buttonDown = e.Direction == mouse.DirPress
				}
				if !buttonDown {
					break
				}
				z := sz.Size()
				x := int(e.X) * N / z.X
				y := int(e.Y) * N / z.Y
				if x < 0 || N <= x || y < 0 || N <= y {
					break
				}
				shared.mouseEvents <- image.Point{x, y}

			case paint.Event:
				publish = buf != nil

			case size.Event:
				sz = e
				closed_window_bound := image.Point{0, 0}
				if closed_window_bound == sz.Bounds().Max {
					os.Exit(0)
				}

			case uploadEvent:
				shared.mu.Lock()
				if buf != nil {
					copy(buf.RGBA().Pix, shared.pix)
					publish = true
				}
				shared.uploadEventSent = false
				shared.mu.Unlock()

				if publish {
					tex.Upload(image.Point{}, buf, buf.Bounds())
				}

			case error:
				log.Print(e)
			}

			if publish {
				w.Scale(sz.Bounds(), tex, tex.Bounds(), draw.Src, nil)
				w.Publish()
			}
		}
	})
}

const (
	pause = false
	play  = true
)

// pauseChan lets the UI event goroutine pause and play the CPU-intensive
// simulation goroutine depending on whether the window is visible (e.g.
// minimized). 64 should be large enough, in typical use, so that the former
// doesn't ever block on the latter.
var pauseChan = make(chan bool, 64)

// uploadEvent signals that the shared pix slice should be uploaded to the
// screen.Texture via the screen.Buffer.
type uploadEvent struct{}

var shared = struct {
	mu              sync.Mutex
	uploadEventSent bool
	mouseEvents     chan image.Point
	pix             []byte
	pix2            []byte
}{
	pix:         make([]byte, 4*N*N),
	mouseEvents: make(chan image.Point),
}
var SATURATION float64

func hue_to_rgb(hue float64) (r_b, g_b, b_b byte) {
	var r, g, b float64
	// these could be passed in
	v := 0.9
	s := SATURATION
	hue = 360 * (math.Mod(hue, 1.0))
	c := v * s
	x := c * (1 - math.Abs(math.Mod(hue/60, 2)-1))
	m := v - c
	switch {
	case hue < 60:
		r, g, b = c, x, 0
	case hue < 120:
		r, g, b = x, c, 0
	case hue < 180:
		r, g, b = 0, c, x
	case hue < 240:
		r, g, b = 0, x, c
	case hue < 300:
		r, g, b = x, 0, c
	case hue < 360:
		r, g, b = c, 0, x
	}
	r_b = byte((r + m) * 255)
	g_b = byte((g + m) * 255)
	b_b = byte((b + m) * 255)
	return
}

type Float64Slice []float64

func (p Float64Slice) Len() int           { return len(p) }
func (p Float64Slice) Less(i, j int) bool { return p[i] < p[j] }
func (p Float64Slice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func run_scenes(q screen.EventDeque) {
	go scene_one(q)
}

func scene_one(q screen.EventDeque) {
	var buf Float64Slice
	var rand_add float64
	var stage byte = INIT_STAGE

	ticker := time.NewTicker(tickDuration)
	stage_timer := time.NewTicker(stageDuration)

	pixel_stage := make([]byte, N*N)
	buf = make([]float64, N*N)

	freeze_map := make([]byte, N*N)
	init_freeze_map(freeze_map)

	rand_val_idx := 0
	random_vals := make([]float64, N)
	for i, _ := range buf {
		buf[i] = rand.Float64()
	}
	for i, _ := range random_vals {
		random_vals[i] = rand.Float64()
	}

	var tickerC <-chan time.Time

	// go func() {
	// 	sat_tick := time.NewTicker(tickDuration).C
	// 	for {
	// 		<-sat_tick
	// 		SATURATION = math.Mod(SATURATION+0.001, 1)
	// 	}
	// }()

	for {
		select {
		case p := <-pauseChan:
			if p == pause {
				tickerC = nil
			} else {
				tickerC = ticker.C
			}
			continue
		case <-shared.mouseEvents:
			go Quicksort(buf)

		case <-stage_timer.C:
			stage = (stage + 1) % MAX_STAGE
			fmt.Println(stage)
			// for i, _ := range buf {
			// 	buf[i] = math.Mod(rand.Float64(), 1)
			// }
		case <-tickerC:
		}

		shared.mu.Lock()
		for y := 0; y < N; y++ {
			for x := 0; x < N; x++ {
				p := (N*y + x) * 4
				hue := buf[y*N+x]
				cur_stage := pixel_stage[y*N+x]

				if cur_stage != stage && math.Mod(hue, 1) < 0.5 {
					// fmt.Println(hue)
					pixel_stage[y*N+x] = stage
					cur_stage = stage
				}
				if freeze_map[y*N+x] == NEAR_FROZEN && math.Mod(hue, 1) < 0.1 {
					freeze_map[y*N+x] = FROZEN
					freeze_neighbors(freeze_map, y*N+x, NEAR_FROZEN)
				}
				if freeze_map[y*N+x] == FROZEN {
					continue
				}

				switch cur_stage {
				case 0:
					// Stage zero - use true random data
					rand_add = rand.Float64()
				case 1:
					rand_add = 0.08
				case 2:
					// rand add is a negative fraction of buffer
					rand_add = hue * -0.05
				case 3:
					// Stage two - pull from a buffer as long as a line
					rand_add = random_vals[rand_val_idx]
					rand_val_idx = (rand_val_idx + 1) % (N)

				case 4:
					// Stage 3 - pull from a buffer 1 less than the length of a line
					rand_add = random_vals[rand_val_idx]
					rand_val_idx = (rand_val_idx + 1) % (N - 1)
					if rand_val_idx == 0 {
						buf[y*N+x] = 0
					}
				case 5:
					// substract square to try to shorten the distribution
					rand_add = hue * hue * -0.05
				}
				buf[y*N+x] += rand_add * 0.05
				shared.pix[p+0] = byte(hue * 255)
				shared.pix[p+1] = byte(0.7 * hue * 255)
				shared.pix[p+2] = byte(0.1 * hue * 255)

				shared.pix[p+3] = 0xff

			}
		}
		uploadEventSent := shared.uploadEventSent
		shared.uploadEventSent = true
		shared.mu.Unlock()

		// q.Send(uploadEvent{})
		if !uploadEventSent {
			q.Send(uploadEvent{})
		}
	}
}

func init_freeze_map(freeze_map []byte) {
	for i := 0; i < N; i++ {
		freeze_map[i] = NEAR_FROZEN
	}
	for i := N * (N - 1); i < N*N; i++ {
		freeze_map[i] = NEAR_FROZEN
	}
	for i := 0; i < N*(N-1); i += N {
		freeze_map[i] = NEAR_FROZEN
		freeze_map[i+N-1] = NEAR_FROZEN
	}
}

func freeze_neighbors(freeze_map []byte, location int, new_setting byte) {
	neighbors := get_neighbors(freeze_map, location)
	for _, n_loc := range neighbors {
		if freeze_map[n_loc] != FROZEN {
			freeze_map[n_loc] = new_setting
		}
	}
	// Second pass - unfreeze all neighbors of frozen cells. this is so we make snakey tubes.
	if new_setting == NEAR_FROZEN {
		for _, n_loc := range neighbors {
			n_freeze := freeze_map[n_loc]
			if n_freeze == FROZEN {
				freeze_neighbors(freeze_map, n_loc, UNFROZEN)
			}
		}
	}
}

func get_neighbors(freeze_map []byte, location int) []int {
	neighbors := []int{location - 1, location + 1,
		location - N, location + N}
	tmp := neighbors[:0]
	for _, loc := range neighbors {
		if loc > 0 && loc < N*N {
			tmp = append(tmp, loc)
		}
	}
	return tmp
}
