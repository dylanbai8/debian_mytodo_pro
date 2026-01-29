package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
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
	// appID 用于系统识别，保持不变
	appID = "io.github.dylan.todo.tray"
	// Socket 文件名，用于单实例检测
	socketFileName = "todo-app.sock"

	maxWeight     = 40 // 输入：20中 / 40英
	maxShowWeight = 40 // 托盘显示：10中 / 20英
)

// 全局变量，用于存储路径
var (
	// configDir 存储可执行文件所在的目录
	configDir string
	// dataFile 存储 todo.json 的完整路径
	dataFile string
	// iconFile 存储 tray.png 的完整路径
	iconFile string
	// socketPath 存储 socket 文件的完整路径
	socketPath string
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
		log.Printf("Error reading todo file: %v", err)
		return []Todo{}
	}
	var todos []Todo
	if err := json.Unmarshal(data, &todos); err != nil {
		log.Printf("Error unmarshalling todo data: %v", err)
		return []Todo{}
	}
	return todos
}

func saveTodos(todos []Todo) {
	data, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		log.Printf("Error marshalling todo data: %v", err)
		return
	}
	if err := os.WriteFile(dataFile, data, 0644); err != nil {
		log.Printf("Error writing todo file: %v", err)
	}
}

func ensureIcon() string {
	if _, err := os.Stat(iconFile); err == nil {
		// 文件已存在，返回绝对路径
		abs, _ := filepath.Abs(iconFile)
		return abs
	}
	// 文件不存在，创建它
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 0}}, image.Point{}, draw.Src)
	black := color.RGBA{0, 0, 0, 255}
	for y := 8; y <= 22; y += 7 {
		for x := 8; x <= 22; x++ {
			img.Set(x, y, black)
		}
	}
	f, err := os.Create(iconFile)
	if err != nil {
		log.Printf("Failed to create icon file: %v", err)
		return "" // 返回空字符串，Fyne可能会使用默认图标
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Printf("Failed to encode icon: %v", err)
	}
	abs, _ := filepath.Abs(iconFile)
	return abs
}

// getExecutableDir 返回可执行文件所在的目录
func getExecutableDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exePath), nil
}

/* ================= 单实例逻辑 ================= */

// runSingleInstanceCheck 检查是否已有实例在运行
// 如果是，则发送信号并退出。如果不是，则启动监听并返回。
// 返回一个布尔值，true表示当前进程是主实例，false表示是副本。
func runSingleInstanceCheck() (bool, error) {
	// 尝试连接到已存在的 socket
	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		// 连接成功，说明已有实例在运行
		defer conn.Close()
		// 发送 "show" 信号
		_, err = conn.Write([]byte("show\n"))
		if err != nil {
			return false, fmt.Errorf("failed to send signal to existing instance: %w", err)
		}
		log.Println("Another instance is already running. Signaling it to show the window and exiting.")
		return false, nil // false 表示不是主实例
	}

	// 连接失败，说明没有实例在运行，当前进程成为主实例
	// 启动一个 goroutine 来监听 socket
	go func() {
		// 清理旧的 socket 文件（如果存在）
		_ = os.Remove(socketPath)

		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			log.Fatalf("Failed to create socket listener: %v", err)
		}
		defer listener.Close()
		log.Printf("Socket listener started at %s", socketPath)

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Socket accept error: %v", err)
				continue
			}
			go handleSocketConnection(conn)
		}
	}()

	return true, nil // true 表示是主实例
}

// handleSocketConnection 处理来自新实例的连接
func handleSocketConnection(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	// 读取一行消息
	message, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Failed to read from socket: %v", err)
		return
	}

	message = strings.TrimSpace(message)
	log.Printf("Received signal from new instance: %s", message)

	if message == "show" {
		// 使用 fyne.Do 确保在主 goroutine 中执行 UI 操作
		fyne.Do(func() {
			// 假设 inputWin 是一个包级变量或可以通过闭包访问
			// 在我们的代码结构中，需要将 inputWin 提升或通过其他方式访问
			// 这里我们通过一个技巧：在 main 函数中定义一个 showWindow 函数
			if showWindow != nil {
				showWindow()
			}
		})
	}
}

/* ================= Main ================= */

// showWindow 是一个函数变量，用于在 socket 信号到达时调用
var showWindow func()

func main() {
	// 1. 初始化路径
	var err error
	configDir, err = getExecutableDir()
	if err != nil {
		// 如果获取失败，使用当前目录作为备选
		log.Printf("Warning: could not get executable directory: %v. Using current directory.", err)
		configDir, _ = os.Getwd()
	}
	dataFile = filepath.Join(configDir, "todo.json")
	iconFile = filepath.Join(configDir, "tray.png")

	// 设置 socket 路径，通常放在用户缓存目录或 /tmp 下更规范
	// 为了简单和权限问题，我们放在 /tmp 下，并加上用户名以避免冲突
	currentUser, err := user.Current()
	if err != nil {
		// 如果获取用户失败，使用一个通用名称
		socketPath = filepath.Join("/tmp", "todo-app.sock")
	} else {
		socketPath = filepath.Join("/tmp", fmt.Sprintf("todo-app-%s.sock", currentUser.Username))
	}

	// 2. 单实例检查
	isMainInstance, err := runSingleInstanceCheck()
	if err != nil {
		log.Fatalf("Single instance check failed: %v", err)
	}
	if !isMainInstance {
		// 如果不是主实例，直接退出
		os.Exit(0)
	}

	// --- 以下是主实例的逻辑 ---

	a := app.NewWithID(appID)
	todos := loadTodos()

	// 将窗口和托盘相关变量定义在 main 作用域内
	var inputWin fyne.Window
	var tray desktop.App
	var rebuildTray func()

	// 定义 showWindow 函数，使其可以被 socket 处理器调用
	showWindow = func() {
		if inputWin != nil {
			inputWin.Show()
			inputWin.RequestFocus()
		}
	}

	inputWin = a.NewWindow("新增待办")
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

	var ok bool
	tray, ok = a.(desktop.App)
	if !ok {
		log.Fatal("不支持托盘")
	}

	rebuildTray = func() {
		fyne.Do(func() {
			var items []*fyne.MenuItem
			items = append(items, fyne.NewMenuItem("➕ 新增待办", func() {
				inputWin.Show()
				inputWin.RequestFocus()
			}))
			items = append(items, fyne.NewMenuItemSeparator())

			if len(todos) == 0 {
				items = append(items, fyne.NewMenuItem("（暂无待办）", nil))
			} else {
				for i := range todos {
					t := todos[i]
					label := "☐ " + truncateByWeightWithEllipsis(t.Text, maxShowWeight)
					items = append(items, fyne.NewMenuItem(label, func(itemText string) func() {
						return func() {
							for idx, item := range todos {
								if item.Text == itemText {
									todos = append(todos[:idx], todos[idx+1:]...)
									break
								}
							}
							saveTodos(todos)
							rebuildTray()
						}
					}(t.Text))) // 使用闭包捕获正确的 todo 项
				}
			}

			items = append(items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem("退出", func() {
				// 清理 socket 文件
				_ = os.Remove(socketPath)
				a.Quit()
			}))
			tray.SetSystemTrayMenu(fyne.NewMenu("Todo", items...))
		})
	}

	entry.OnSubmitted = func(text string) {
		if text == "" {
			return
		}
		todos = append(todos, Todo{Text: text})
		saveTodos(todos)
		entry.SetText("")
		showSuccess()
		rebuildTray()
	}

	iconPath := ensureIcon()
	if iconPath == "" {
		log.Println("Could not find or create tray icon. The app will run without it.")
	} else {
		res, _ := fyne.LoadResourceFromPath(iconPath)
		tray.SetSystemTrayIcon(res)
	}
	rebuildTray()

	// 确保在应用退出时清理 socket 文件
	defer func() {
		_ = os.Remove(socketPath)
	}()

	a.Run()
}
