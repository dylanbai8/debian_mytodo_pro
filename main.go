package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const (
	iconFile      = "tray.png"
	dataFile      = "todo.json"
	appID         = "io.github.dylan.todo.tray"
	maxWeight     = 40 // 输入：20中 / 40英
	maxShowWeight = 20 // 托盘显示：10中 / 20英
)

type Todo struct {
	Text string `json:"text"`
}

/* ================= 工具函数 ================= */

// 计算混合权重：中文2，其他1
func getWeight(s string) int {
	w := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			w += 2
		} else {
			w += 1
		}
	}
	return w
}

// 根据权重截断字符串，如果被截断则添加省略号
func truncateByWeightWithEllipsis(s string, maxW int) string {
	if getWeight(s) <= maxW {
		return s
	}
	
	currW := 0
	res := ""
	for _, r := range s {
		itemW := 1
		if unicode.Is(unicode.Han, r) {
			itemW = 2
		}
		if currW+itemW > maxW {
			break
		}
		currW += itemW
		res += string(r)
	}
	return res + "…"
}

// 基础截断（不带省略号，用于输入框强制限制）
func truncateByWeight(s string, maxW int) string {
	currW := 0
	res := ""
	for _, r := range s {
		itemW := 1
		if unicode.Is(unicode.Han, r) {
			itemW = 2
		}
		if currW+itemW > maxW {
			break
		}
		currW += itemW
		res += string(r)
	}
	return res
}

/* ================= 数据读写 ================= */

func loadTodos() []Todo {
	if _, err := os.Stat(dataFile); os.IsNotExist(err) {
		return []Todo{}
	}
	data, err := os.ReadFile(dataFile)
	if err != nil {
		return []Todo{}
	}
	var todos []Todo
	_ = json.Unmarshal(data, &todos)
	return todos
}

func saveTodos(todos []Todo) {
	data, _ := json.MarshalIndent(todos, "", "  ")
	_ = os.WriteFile(dataFile, data, 0644)
}

func ensureIcon() string {
	if _, err := os.Stat(iconFile); err == nil {
		abs, _ := filepath.Abs(iconFile)
		return abs
	}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 0}}, image.Point{}, draw.Src)
	black := color.RGBA{0, 0, 0, 255}
	for y := 8; y <= 22; y += 7 {
		for x := 8; x <= 22; x++ {
			img.Set(x, y, black)
		}
	}
	f, _ := os.Create(iconFile)
	defer f.Close()
	_ = png.Encode(f, img)
	abs, _ := filepath.Abs(iconFile)
	return abs
}

/* ================= Main ================= */

func main() {
	a := app.NewWithID(appID)
	todos := loadTodos()

	inputWin := a.NewWindow("新增待办")
	entry := widget.NewEntry()
	entry.SetPlaceHolder("输入待办事项...")

	leftTips := canvas.NewText(fmt.Sprintf("剩余: %d", maxWeight), color.NRGBA{128, 128, 128, 255})
	leftTips.TextSize = 10

	rightTips := canvas.NewText("按回车提交", color.NRGBA{150, 150, 150, 200})
	rightTips.TextSize = 10
	rightTips.Alignment = fyne.TextAlignTrailing

	showSuccess := func() {
		rightTips.Text = "√ 待办已提交"
		rightTips.Color = color.NRGBA{50, 205, 50, 255}
		rightTips.Refresh()
		go func() {
			time.Sleep(time.Second * 2)
			fyne.Do(func() {
				rightTips.Text = "按回车提交"
				rightTips.Color = color.NRGBA{150, 150, 150, 200}
				rightTips.Refresh()
			})
		}()
	}

	entry.OnChanged = func(s string) {
		currentW := getWeight(s)
		if currentW > maxWeight {
			entry.SetText(truncateByWeight(s, maxWeight))
			return
		}
		leftTips.Text = fmt.Sprintf("剩余: %d", maxWeight-currentW)
		leftTips.Refresh()
	}

	bottomBar := container.New(layout.NewHBoxLayout(), 
		leftTips, 
		layout.NewSpacer(), 
		rightTips,
	)
	content := container.NewPadded(
		container.NewBorder(nil, bottomBar, nil, nil, entry),
	)

	inputWin.SetContent(content)
	inputWin.Resize(fyne.NewSize(320, 85))
	inputWin.SetFixedSize(true)
	inputWin.SetCloseIntercept(func() { inputWin.Hide() })

	tray, ok := a.(desktop.App)
	if !ok {
		log.Fatal("不支持托盘")
	}

	var rebuildTray func()
	rebuildTray = func() {
		fyne.Do(func() {
			var items []*fyne.MenuItem
			items = append(items, fyne.NewMenuItem("➕ 新增待办", func() {
				inputWin.Show()
				inputWin.RequestFocus()
			}), fyne.NewMenuItemSeparator())

			if len(todos) == 0 {
				items = append(items, fyne.NewMenuItem("（暂无待办）", nil))
			} else {
				for i := range todos {
					t := todos[i]
					// 使用新的权重截断函数，实现中10英20的显示限制
					label := "☐ " + truncateByWeightWithEllipsis(t.Text, maxShowWeight)
					items = append(items, fyne.NewMenuItem(label, func() {
						for idx, item := range todos {
							if item.Text == t.Text {
								todos = append(todos[:idx], todos[idx+1:]...)
								break
							}
						}
						saveTodos(todos)
						rebuildTray()
					}))
				}
			}

			items = append(items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem("退出", func() { a.Quit() }))
			tray.SetSystemTrayMenu(fyne.NewMenu("Todo", items...))
		})
	}

	entry.OnSubmitted = func(text string) {
		if text == "" { return }
		todos = append(todos, Todo{Text: text})
		saveTodos(todos)
		entry.SetText("") 
		showSuccess()
		rebuildTray()
	}

	iconPath := ensureIcon()
	res, _ := fyne.LoadResourceFromPath(iconPath)
	tray.SetSystemTrayIcon(res)
	rebuildTray()

	a.Run()
}
