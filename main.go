package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	iconFile = "tray.png"
	dataFile = "todo.json"
	appID    = "io.github.dylan.todo.tray"

	maxLen  = 50 // 输入最大长度
	showLen = 20 // 托盘显示最大长度
)

type Todo struct {
	Text string `json:"text"`
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

/* ================= 图标 ================= */

func ensureIcon() string {
	if _, err := os.Stat(iconFile); err == nil {
		abs, _ := filepath.Abs(iconFile)
		return abs
	}

	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(),
		&image.Uniform{color.RGBA{0, 0, 0, 0}},
		image.Point{}, draw.Src)

	black := color.RGBA{0, 0, 0, 255}
	for y := 8; y <= 22; y += 7 {
		for x := 8; x <= 22; x++ {
			img.Set(x, y, black)
		}
	}

	f, err := os.Create(iconFile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	_ = png.Encode(f, img)

	abs, _ := filepath.Abs(iconFile)
	return abs
}

/* ================= 工具 ================= */

func shorten(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

/* ================= main ================= */

func main() {
	a := app.NewWithID(appID)
	todos := loadTodos()

	// 新增窗口
	inputWin := a.NewWindow("新增待办")
	entry := widget.NewEntry()
	entry.SetPlaceHolder("输入待办事项（最多 50 字，回车确认）")
	inputWin.SetContent(entry)
	inputWin.Resize(fyne.NewSize(320, 60))
	inputWin.SetFixedSize(true)

	// 点击右上角 X 只隐藏窗口，不销毁
	inputWin.SetCloseIntercept(func() {
		inputWin.Hide()
	})

	tray, ok := a.(desktop.App)
	if !ok {
		log.Fatal("当前平台不支持系统托盘")
	}

	var rebuildTray func()

	// 回车新增，不关闭窗口
	entry.OnSubmitted = func(text string) {
		if text == "" {
			return
		}
		if utf8.RuneCountInString(text) > maxLen {
			entry.SetText("")
			return
		}
		fyne.Do(func() {
			todos = append(todos, Todo{Text: text})
			saveTodos(todos)
			entry.SetText("") // 清空输入框
			rebuildTray()
		})
	}

	// 构建托盘菜单
	rebuildTray = func() {
		var items []*fyne.MenuItem

		items = append(items,
			fyne.NewMenuItem("➕ 新增待办", func() {
				fyne.Do(func() {
					inputWin.Show()
					inputWin.RequestFocus()
				})
			}),
			fyne.NewMenuItemSeparator(),
		)

		if len(todos) == 0 {
			items = append(items, fyne.NewMenuItem("（暂无待办）", nil))
		} else {
			for _, todo := range todos {
				text := todo.Text
				label := "☐ " + shorten(text, showLen)

				items = append(items, fyne.NewMenuItem(label, func() {
					fyne.Do(func() {
						// 按文本删除
						for i, t := range todos {
							if t.Text == text {
								todos = append(todos[:i], todos[i+1:]...)
								break
							}
						}
						saveTodos(todos)
						rebuildTray()
					})
				}))
			}
		}

		items = append(items,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("退出", func() {
				fyne.Do(func() {
					a.Quit()
				})
			}),
		)

		tray.SetSystemTrayMenu(fyne.NewMenu("Todo", items...))
	}

	// 托盘初始化
	iconPath := ensureIcon()
	res, err := fyne.LoadResourceFromPath(iconPath)
	if err != nil {
		log.Fatal("托盘图标加载失败:", err)
	}
	tray.SetSystemTrayIcon(res)

	rebuildTray()

	a.Run()
}
