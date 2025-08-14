package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-vgo/robotgo"
)

/*
Cấu hình:
- REGION_X, Y, W, H: vùng cần chụp (tọa độ màn hình)
- TOLERANCE: sai khác màu cho phép (0..255)
- SCAN_STEP / SAMPLE_STEP: bước quét thô / bước xác minh (tăng để nhanh hơn)
*/
const (
	REGION_X = 0
	REGION_Y = 0
	REGION_W = 730
	REGION_H = 1080

	TOLERANCE   = 22 // sai số màu cho so khớp
	SCAN_STEP   = 1
	SAMPLE_STEP = 2

	INTERVAL_SEC  = 30 // số giây giữa mỗi lần quét
	CLICK_HOLD_MS = 70 // thời gian giữ chuột
)

func mustMkDir(p string) {
	_ = os.MkdirAll(p, 0755)
}

type IconCfg struct {
	Name  string   `json:"name"`
	Files []string `json:"file"` // Đổi từ string sang []string
}

type ActionCfg struct {
	Type string `json:"type"`
}

func main() {
	fmt.Println("== Tool chụp icon (CLI) ==")
	printHelp()
	var x1, y1, x2, y2 int
	var haveTL, haveBR bool

	mustMkDir("icons")

	sc := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		cmd := strings.ToLower(fields[0])

		switch cmd {
		case "help", "h", "?":
			printHelp()

		case "pos":
			x, y := robotgo.Location()
			fmt.Printf("Mouse at (%d,%d)\n", x, y)

		case "test":
			img, err := robotgo.CaptureImg(REGION_X, REGION_H/2, REGION_W, REGION_H)
			if err != nil {
				fmt.Printf("CaptureImg ROI error: %v\n", err)
				return
			}
			f, err := os.Create("roi.png")
			if err != nil {
				fmt.Println("Tạo file lỗi:", err)
				return
			}
			defer f.Close()
			if err := png.Encode(f, img); err != nil {
				fmt.Println("Ghi PNG lỗi:", err)
				return
			}

		case "tl":
			x1, y1 = robotgo.Location()
			haveTL = true
			fmt.Printf("Set TL = (%d,%d)\n", x1, y1)

		case "br":
			x2, y2 = robotgo.Location()
			haveBR = true
			fmt.Printf("Set BR = (%d,%d)\n", x2, y2)
		case "icon":
			if !haveTL || !haveBR {
				fmt.Println("ROI chưa đủ 2 góc. Dùng: tl, rồi br")
				continue
			}
			nx1, ny1, nx2, ny2 := normalizeRect(x1, y1, x2, y2)
			fmt.Printf("ICON TL=(%d,%d) BR=(%d,%d) size=(%dx%d)\n", nx1, ny1, nx2, ny2, nx2-nx1, ny2-ny1)
		case "save":
			if !haveTL || !haveBR {
				fmt.Println("ROI chưa đủ 2 góc. Dùng: tl, rồi br")
				continue
			}
			filename := ""
			if len(fields) >= 2 {
				filename = fields[1] + ".png"
			} else {
				filename = time.Now().Format("20060102-150405.000") + ".png"
			}
			if !strings.HasSuffix(strings.ToLower(filename), ".png") {
				filename += ".png"
			}
			nx1, ny1, nx2, ny2 := normalizeRect(x1, y1, x2, y2)
			w, h := nx2-nx1, ny2-ny1
			if w <= 0 || h <= 0 {
				fmt.Println("ROI không hợp lệ (w/h <= 0). Đặt lại tl, br.")
				continue
			}
			img, err := robotgo.CaptureImg(nx1, ny1, w, h)
			if err != nil {
				fmt.Println("CaptureImg lỗi:", err)
				continue
			}
			path := filepath.Join("icons", filename)
			f, err := os.Create(path)
			if err != nil {
				fmt.Println("Tạo file lỗi:", err)
				continue
			}
			if err := png.Encode(f, img); err != nil {
				_ = f.Close()
				fmt.Println("Ghi PNG lỗi:", err)
				continue
			}
			_ = f.Close()
			fmt.Printf("Đã lưu: %s (x=%d,y=%d,w=%d,h=%d)\n", path, nx1, ny1, w, h)

		case "exit", "quit", "q":
			fmt.Println("Bye!")
			return
		case "autohs":
			fmt.Println("\nBắt đầu quét màn hình")
			icons, err := LoadIconsConfig("icons.json")
			if err != nil {
				fmt.Println("Lỗi đọc icons.json:", err)
				return
			}

			type tgt struct {
				name string
				img  image.Image
				w, h int
			}
			var targets []tgt
			for _, icon := range icons {
				if icon.Name != "hs" {
					continue
				}
				for _, file := range icon.Files {
					im, err := loadPNG(file)
					if err != nil {
						fmt.Printf("Không load được %s: %v (bỏ qua)\n", file, err)
						continue
					}
					b := im.Bounds()
					targets = append(targets, tgt{name: file, img: im, w: b.Dx(), h: b.Dy()})
				}
			}
			if len(targets) == 0 {
				fmt.Println("Không có icon nào hợp lệ để quét.")
				break
			}
			fmt.Printf("targets: %v\n", targets)
			for {
				// 1) Chụp đúng ROI thay vì full màn hình
				img, err := robotgo.CaptureImg(REGION_X, REGION_Y, REGION_W, REGION_H)
				if err != nil {
					fmt.Printf("CaptureImg ROI error: %v\n", err)
					return
				}
				matched := false
				for _, t := range targets {
					startT := time.Now()
					rx, ry, ok := findSubImageFast(img, t.img, TOLERANCE, SCAN_STEP, SAMPLE_STEP)

					if !ok || matched {
						continue
					}

					// Tính toạ độ click giữa icon
					cx := REGION_X + rx + t.w/2
					cy := REGION_Y + ry + t.h/2

					// Lưu vị trí chuột rồi trả lại sau khi click (để không làm phiền bạn)
					curX, curY := robotgo.Location()
					robotgo.MouseSleep = CLICK_HOLD_MS
					robotgo.Move(cx, cy)
					robotgo.Click("left", true)
					robotgo.Move(curX, curY)
					elapsed := time.Since(startT)
					fmt.Printf("[HIT] %s @ ROI(%d,%d) ABS(%d,%d) (%.1fms)\n",
						t.name, rx, ry, cx, cy, float64(elapsed.Milliseconds()))
					matched = true
					break // ưu tiên icon theo thứ tự: dừng ở icon đầu tiên trúng
				}
				time.Sleep(INTERVAL_SEC * time.Second)

			}
		case "autoptt":
			fmt.Println("\nBắt đầu làm PTT")
			icons, err := LoadIconsConfig("icons.json")
			if err != nil {
				fmt.Println("Lỗi đọc icons.json:", err)
				return
			}

			type tgt struct {
				name string
				img  image.Image
				w, h int
			}
			var targets []tgt
			for _, icon := range icons {
				if icon.Name != "ptt" {
					continue
				}
				for _, file := range icon.Files {
					im, err := loadPNG(file)
					if err != nil {
						fmt.Printf("Không load được %s: %v (bỏ qua)\n", file, err)
						continue
					}
					b := im.Bounds()
					targets = append(targets, tgt{name: file, img: im, w: b.Dx(), h: b.Dy()})
				}
			}
			if len(targets) == 0 {
				fmt.Println("Không có icon nào hợp lệ để quét.")
				break
			}
			for {
				// 1) Chụp đúng ROI thay vì full màn hình
				for _, t := range targets {
					startT := time.Now()
					img, err := robotgo.CaptureImg(REGION_X, REGION_Y, REGION_W, REGION_H)
					if err != nil {
						fmt.Printf("CaptureImg ROI error: %v\n", err)
						return
					}
					rx, ry, ok := findSubImageFast(img, t.img, TOLERANCE, SCAN_STEP, SAMPLE_STEP)
					if !ok {
						continue
					}

					// Tính toạ độ click giữa icon
					cx := REGION_X + rx + t.w/2
					cy := REGION_Y + ry + t.h/2

					// Lưu vị trí chuột rồi trả lại sau khi click (để không làm phiền bạn)
					curX, curY := robotgo.Location()
					robotgo.MouseSleep = CLICK_HOLD_MS
					robotgo.Move(cx, cy)
					robotgo.Click("left", true)
					robotgo.Move(curX, curY)
					elapsed := time.Since(startT)
					fmt.Printf("[HIT] %s @ ROI(%d,%d) ABS(%d,%d) (%.1fms)\n",
						t.name, rx, ry, cx, cy, float64(elapsed.Milliseconds()))
					time.Sleep(2 * time.Second)
				}
				time.Sleep(INTERVAL_SEC * time.Second)
			}
		default:
			fmt.Printf("Không hiểu lệnh: %s (gõ 'help' để xem hướng dẫn)\n", cmd)
		}
	}
}

func normalizeRect(x1, y1, x2, y2 int) (nx1, ny1, nx2, ny2 int) {
	nx1, ny1, nx2, ny2 = x1, y1, x2, y2
	if nx2 < nx1 {
		nx1, nx2 = nx2, nx1
	}
	if ny2 < ny1 {
		ny1, ny2 = ny2, ny1
	}
	return
}

func printHelp() {
	fmt.Println(`Lệnh:
	tl            - đặt góc trái-trên từ vị trí chuột hiện tại
	br            - đặt góc phải-dưới từ vị trí chuột hiện tại
	roi           - xem lại ROI đã chọn
	pos           - in toạ độ chuột hiện tại
	save [ten]    - chụp và lưu vào icons
	help          - hiện hướng dẫn
	exit          - thoát
	autohs        - auto shake hands
	autoptt       - auto làm ptt`)
}

/* ==========================
   Helpers: load/save PNG
   ========================== */

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

/* ==========================================
   Thuật toán tìm nhanh subimage trong ROI
   - 2 pha: lọc nhanh theo anchors, rồi xác minh
   - TOLERANCE: sai khác màu theo kênh RGBA
   - SCAN_STEP/SAMPLE_STEP: trade-off tốc độ/độ chính xác
   ========================================== */

func findSubImageFast(screen image.Image, target image.Image, tolerance int, scanStep, sampleStep int) (int, int, bool) {
	sb := screen.Bounds()
	tb := target.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	tw, th := tb.Dx(), tb.Dy()
	if tw == 0 || th == 0 || sw < tw || sh < th {
		return -1, -1, false
	}

	anchors := [][2]int{{0, 0}, {tw - 1, 0}, {0, th - 1}, {tw - 1, th - 1}}
	if tw >= 5 && th >= 5 {
		anchors = append(anchors, [2]int{tw / 2, th / 2})
	}

	for y := 0; y <= sh-th; y += max(1, scanStep) {
		for x := 0; x <= sw-tw; x += max(1, scanStep) {
			// so nhanh pixel (0,0)
			if !almostEqualRGBA(
				screen.At(sb.Min.X+x, sb.Min.Y+y),
				target.At(tb.Min.X, tb.Min.Y),
				tolerance,
			) {
				continue
			}
			// check anchors
			ok := true
			for _, a := range anchors {
				ax, ay := a[0], a[1]
				if !almostEqualRGBA(
					screen.At(sb.Min.X+x+ax, sb.Min.Y+y+ay),
					target.At(tb.Min.X+ax, tb.Min.Y+ay),
					tolerance,
				) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			// xác minh theo mẫu
			if verifyMatch(screen, target, x, y, tolerance, sampleStep) {
				return x, y, true
			}
		}
	}
	return -1, -1, false
}

func verifyMatch(screen image.Image, target image.Image, offX, offY int, tol, step int) bool {
	sb := screen.Bounds()
	tb := target.Bounds()
	tw, th := tb.Dx(), tb.Dy()

	for ty := 0; ty < th; ty += max(1, step) {
		for tx := 0; tx < tw; tx += max(1, step) {
			if !almostEqualRGBA(
				screen.At(sb.Min.X+offX+tx, sb.Min.Y+offY+ty),
				target.At(tb.Min.X+tx, tb.Min.Y+ty),
				tol,
			) {
				return false
			}
		}
	}
	// kiểm thêm mép dưới/phải khi step > 1
	if step > 1 {
		for tx := 0; tx < tw; tx += step {
			if !almostEqualRGBA(
				screen.At(sb.Min.X+offX+tx, sb.Min.Y+offY+th-1),
				target.At(tb.Min.X+tx, tb.Min.Y+th-1),
				tol,
			) {
				return false
			}
		}
		for ty := 0; ty < th; ty += step {
			if !almostEqualRGBA(
				screen.At(sb.Min.X+offX+tw-1, sb.Min.Y+offY+ty),
				target.At(tb.Min.X+tw-1, tb.Min.Y+ty),
				tol,
			) {
				return false
			}
		}
	}
	return true
}

func almostEqualRGBA(c1, c2 color.Color, tol int) bool {
	R1, G1, B1, A1 := c1.RGBA()
	R2, G2, B2, A2 := c2.RGBA()
	return absInt(int(R1>>8)-int(R2>>8)) <= tol &&
		absInt(int(G1>>8)-int(G2>>8)) <= tol &&
		absInt(int(B1>>8)-int(B2>>8)) <= tol &&
		absInt(int(A1>>8)-int(A2>>8)) <= tol
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func LoadIconsConfig(path string) ([]*IconCfg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var icons []*IconCfg
	if err := json.NewDecoder(f).Decode(&icons); err != nil {
		return nil, err
	}

	return icons, nil
}
