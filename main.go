package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	app "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

//go:embed stunde.png
var iconRunningBytes []byte

//this embeds the icon into the exe file

//go:embed pause.png
var iconIdleBytes []byte

// this embeds the idle icon into the exe file
const (
	DESKTOP_SWITCHDESKTOP int = 0x0100 // The access to the desktop
)

var shouldCount atomic.Bool

var (
	//load kernel32 and user32 DLLs
	user32   = syscall.MustLoadDLL("user32.dll")
	kernel32 = syscall.MustLoadDLL("kernel32.dll")
	//load processes from user32.dll and kernel32.dll for computation of last input time
	getLastInputInfo = user32.MustFindProc("GetLastInputInfo")
	getTickCount     = kernel32.MustFindProc("GetTickCount")
	//struct to use a return value from getLastInputInfo
	lastInputInfo struct {
		cbSize uint32
		dwTime uint32
	}
)

type IdleTimeOut struct {
	lock  sync.RWMutex
	value time.Duration
}

func (it *IdleTimeOut) Set(minutes int64) {
	it.lock.Lock()
	defer it.lock.Unlock()
	it.value = time.Duration(minutes) * time.Minute
}

func (it *IdleTimeOut) Get() time.Duration {
	it.lock.RLock()
	defer it.lock.RUnlock()
	to := it.value
	return to
}

var IDLE_TIMEOUT = IdleTimeOut{sync.RWMutex{}, time.Duration(6) * time.Minute}

// contains a start and an end point to define a time span
type TimeSpan struct {
	start time.Time
	end   time.Time
}

// contains the time spans in which the user has worked and a lock to synchronize actions
type WorkTimer struct {
	lock      sync.RWMutex
	timeSpans []TimeSpan
}

// adds a new time span and sets its start to time.Now()
func (wt *WorkTimer) SetNewTimeSpanStart() {
	wt.lock.Lock()
	defer wt.lock.Unlock()
	var ts TimeSpan
	ts.start = time.Now()
	wt.timeSpans = append(wt.timeSpans, ts)
}

// sets the end point as time.Now() in the current time span
func (wt *WorkTimer) SetCurrentTimeSpanEnd() {
	wt.lock.Lock()
	defer wt.lock.Unlock()
	wt.timeSpans[len(wt.timeSpans)-1].end = time.Now()
}

// takes all existing time spans and adds up the times between the end and start points.
// return a duration
func (wt *WorkTimer) GetPreliminaryWorkTimeDuration() time.Duration {
	wt.lock.RLock()
	defer wt.lock.RUnlock()
	timeWorked := time.Duration(0)
	for _, ts := range wt.timeSpans {
		if ts.end.IsZero() {
			timeWorked += time.Now().Sub(ts.start)
		} else {
			timeWorked += ts.end.Sub(ts.start)
		}
	}
	return timeWorked
}

func (wt *WorkTimer) GetPreliminaryWorkTimeSeconds() int64 {
	return int64(wt.GetPreliminaryWorkTimeDuration().Seconds())
}

// resets the timer by deleting all existing time spans and adding a new one with a start point
func (wt *WorkTimer) Reset() {
	wt.lock.Lock()
	wt.timeSpans = []TimeSpan{}
	wt.lock.Unlock()
	wt.SetNewTimeSpanStart()
}

func main() {
	//setup Logging
	//define log file
	f, err := os.OpenFile("times.txt", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	//initiate logger
	logger := log.New(f, "timeLog: ", 0)

	//initiate thread safe minute counter and idle status
	var wTimer WorkTimer
	var isIdle atomic.Bool
	var errorsFromSyscalls atomic.Value

	shouldCount.Store(true)
	wTimer.SetNewTimeSpanStart()
	isIdle.Store(false)
	errorsFromSyscalls.Store("")
	//start counting minutes in separate thread
	go calculateTimeSpans(&wTimer, &isIdle, &errorsFromSyscalls)

	//define app window
	a := app.New()
	w := a.NewWindow("Work Timer")

	iconRunning := fyne.NewStaticResource("iconRunning", iconRunningBytes)
	iconIdle := fyne.NewStaticResource("iconIdle", iconIdleBytes)

	//define and set icon
	w.SetIcon(iconRunning)

	//Define Menu Options to display startStop, timeout settings, etc.
	menuItemTimeOut := fyne.NewMenuItem("Set Time-Out", func() {
		wTimeOut := a.NewWindow("Set Time-Out")
		input := widget.NewEntry()
		input.Validator = func(s string) error {
			_, err := strconv.ParseFloat(s, 64)
			return err
		}

		//define label
		timeOutLabel := widget.NewLabel("Please enter the time-out in minutes")

		//set placeholder of textbox to current IDLE_TIMEOUT
		input.SetPlaceHolder(strconv.FormatFloat(IDLE_TIMEOUT.value.Minutes(), 'f', 0, 64))

		//define a button to save timeout entered by user and close the window
		saveTimeOut := widget.NewButton("Save", func() {
			numFloat, _ := strconv.ParseFloat(input.Text, 64)
			IDLE_TIMEOUT.Set(int64(numFloat))
			wTimeOut.Close()
		})
		saveTimeOut.Disable()
		//define what happens when the input changes
		//this is used to validate the input and
		//disable the save button when the input can't be parsed into a float
		//or enable it, if the entry is numeric
		input.OnChanged = func(s string) {
			err := input.Validate()
			if err != nil {
				saveTimeOut.Disable()
			} else {
				saveTimeOut.Enable()
			}
		}
		//set all widgets as content of the window to display
		content := container.NewVBox(timeOutLabel, input, saveTimeOut)
		wTimeOut.SetContent(content)
		wTimeOut.SetIcon(w.Icon())
		wTimeOut.CenterOnScreen()
		wTimeOut.Show()
	})

	menuItemResetTimer := fyne.NewMenuItem("Reset Timer", func() {
		dialog.NewConfirm("Resetting Time", "Do you want to reset the timer?", func(userConfirmed bool) {
			if userConfirmed {
				wTimer.Reset()

			}
		}, w).Show()
	})

	//define main menu which holds the options to define a custom timeout and to start/stop the timer
	var menuItemStartStop fyne.MenuItem
	//add the menu options to the menu
	menuOptions := fyne.NewMenu("Options", menuItemTimeOut, menuItemResetTimer)
	menuMain := fyne.NewMenu("Main", &menuItemStartStop)

	mainMenu := fyne.NewMainMenu(menuMain, menuOptions)
	//define the Start/Stop Button after the main menu because
	//the start stop button needs to be able to update the main menu itself
	menuItemStartStop.Label = "Stop"
	menuItemStartStop.Action = func() {
		if menuItemStartStop.Label == "Stop" {
			//if the Menu Option is on Stop then we stop the timer
			shouldCount.CompareAndSwap(true, false)
			wTimer.SetCurrentTimeSpanEnd()
			//change the icon to idle mode
			w.SetIcon(iconIdle)
			if desk, ok := a.(desktop.App); ok {
				desk.SetSystemTrayIcon(iconIdle)
			}
			//set the option to say Start and refresh the main menu to make the changes visible
			menuItemStartStop.Label = "Start"
			menuMain.Refresh()
		} else {
			//and vice versa
			shouldCount.CompareAndSwap(false, true)
			wTimer.SetNewTimeSpanStart()
			w.SetIcon(iconRunning)
			if desk, ok := a.(desktop.App); ok {
				desk.SetSystemTrayIcon(iconRunning)
			}
			menuItemStartStop.Label = "Stop"
			menuMain.Refresh()
		}

	}

	w.SetMainMenu(mainMenu)

	//define System Tray
	if desk, ok := a.(desktop.App); ok {
		trayOptionShowTime := fyne.NewMenuItem("Show", func() {
			w.Show()
		})
		trayOptionCopyTime := fyne.NewMenuItem("Copy Time", func() {
			timeText := formatSecondsForClipboard(&wTimer)
			w.Clipboard().SetContent(timeText)
		})
		trayMenu := fyne.NewMenu("Work Timer", trayOptionShowTime, trayOptionCopyTime)
		desk.SetSystemTrayMenu(trayMenu)
		desk.SetSystemTrayIcon(iconRunning)
	}

	//intercept close to move to system tray
	w.SetCloseIntercept(func() {
		w.Hide()
	})

	//define label to display current time worked
	timeLabel := binding.NewString()
	timeLabel.Set("Work Time: 0s")
	copyButton := widget.NewButton("Copy Time", func() {
		timeText := formatSecondsForClipboard(&wTimer)
		w.Clipboard().SetContent(timeText)
	})
	errorLabel := binding.NewString()
	errorLabel.Set("")

	timeLabelStart := binding.NewString()
	timeLabelEnd := binding.NewString()
	timeLabelEnd.Set("")

	wTimer.lock.RLock()
	timeLabelStart.Set(wTimer.timeSpans[0].start.Format(time.Kitchen) + "\n")
	wTimer.lock.RUnlock()

	col1 := container.New(layout.NewVBoxLayout(), copyButton, widget.NewLabelWithData(timeLabelStart), layout.NewSpacer())
	col2 := container.New(layout.NewVBoxLayout(), widget.NewLabelWithData(timeLabel), widget.NewLabelWithData(timeLabelEnd), layout.NewSpacer(), widget.NewLabelWithData(errorLabel))
	content := container.New(layout.NewHBoxLayout(), col1, layout.NewSpacer(), col2)
	w.SetContent(content)

	//run a thread to keep timer updated
	go func() {
		for {
			time.Sleep(time.Millisecond * 500)
			if shouldCount.Load() {
				if isIdle.Load() {
					dur := time.Duration(wTimer.GetPreliminaryWorkTimeSeconds()) * time.Second
					timeLabel.Set("---Currently idle!---\n Work Time until now: " + dur.String())
					w.SetIcon(iconIdle)
					if desk, ok := a.(desktop.App); ok {
						desk.SetSystemTrayIcon(iconIdle)
					}
				} else {
					dur := time.Duration(wTimer.GetPreliminaryWorkTimeSeconds()) * time.Second
					timeLabel.Set("Work Time: " + dur.String())
					w.SetIcon(iconRunning)
					if desk, ok := a.(desktop.App); ok {
						desk.SetSystemTrayIcon(iconRunning)
					}
				}
			}
		}
	}()
	//run a thread to keep errors updated
	go func() {
		for {
			time.Sleep(time.Second * 2)
			errText, ok := errorsFromSyscalls.Load().(string)
			if ok {
				if errText != "" {
					errorLabel.Set(errText)
				}
			}
		}
	}()

	//minimize the window 2 seconds after startup.
	go func() {
		time.Sleep(time.Second * 2)
		w.Hide()
		//customize quit function

		for _, item := range w.MainMenu().Items {
			if item.Label == "Main" {
				for _, option := range item.Items {
					fmt.Println("item: " + option.Label)
					if option.IsQuit {
						fmt.Println("quit found")
						option.Action = func() {
							fmt.Println("func triggered")
							d := dialog.NewConfirm("Attention", "Do you want to exit the app?", func(shouldQuit bool) {
								if shouldQuit {
									a.Quit()
								}
							}, w)
							d.Show()
						}
					}
				}
			}
		}
	}()

	go func() {
		for {
			time.Sleep(time.Millisecond * 500)
			wTimer.lock.RLock()
			startText := ""
			endText := ""
			for _, ts := range wTimer.timeSpans {
				startText = startText + ts.start.Format(time.Kitchen) + "\n"
				if !ts.end.IsZero() {
					endText = endText + ts.end.Format(time.Kitchen) + "\n"
				}
			}
			wTimer.lock.RUnlock()
			timeLabelStart.Set(startText)
			timeLabelEnd.Set(endText)
		}
	}()

	//set initial window size
	w.Resize(fyne.NewSize(260, 190))

	//log time when programm closes
	defer logger.Println(time.Now().Format(time.RFC822) + " worked for " + formatSecondsForClipboard(&wTimer))

	//start process and run main GUI loop
	w.CenterOnScreen()
	w.ShowAndRun()
}

// calculates the seconds the user has worked
// does not increment when the screen is locked or no input e.g. mouse movement have been detected for more than 5 minutes
func calculateTimeSpans(wTimer *WorkTimer, isIdle *atomic.Bool, errorsFromSyscalls *atomic.Value) {
	//create a ticker, that triggers every second
	t := time.NewTicker(1 * time.Second)
	//if true then CurrentTimeSpanEnd ist set once upon entering idle mode
	//if false then NewTimeSpanStart ist set once upon exiting idle mode
	saveEndOrSaveStart := true
	for range t.C {
		if isIdleOrLocked(errorsFromSyscalls) {
			//while Idle
			if saveEndOrSaveStart {
				//safe the end time of the current time span once
				wTimer.SetCurrentTimeSpanEnd()
				//set bool to false to ensure end time is only set once
				saveEndOrSaveStart = false
			}
			isIdle.CompareAndSwap(false, true)
		} else {
			if !saveEndOrSaveStart {
				//bool can only be false if it was previously set by the idle branch above
				//thus we need to start a new time span
				wTimer.SetNewTimeSpanStart()
			}
			//bool is set to true too ensure the above actions are only ever triggered if
			//idle mode is entered or exited but not during regular counting mode
			saveEndOrSaveStart = true
			isIdle.CompareAndSwap(true, false)
		}
	}
}

// returns true if desktop is locked or user has not made an input for IDLE_TIMEOUT minutes
func isIdleOrLocked(errorsFromSyscalls *atomic.Value) bool {
	if getTimeSinceLastInput(errorsFromSyscalls) <= IDLE_TIMEOUT.Get() && !screenIsLocked(errorsFromSyscalls) {
		return false
	} else {
		return true
	}
}

// queries the time since last user input (mouse movement, button presses, etc.)
func getTimeSinceLastInput(errorsFromSyscalls *atomic.Value) time.Duration {
	lastInputInfo.cbSize = uint32(unsafe.Sizeof(lastInputInfo))
	currentTickCount, _, _ := getTickCount.Call()
	r1, _, _ := getLastInputInfo.Call(uintptr(unsafe.Pointer(&lastInputInfo)))
	if r1 == 0 {
		errorsFromSyscalls.CompareAndSwap("", "could not call getLastInputInfo")
		return time.Duration(0)
	}
	errorsFromSyscalls.CompareAndSwap("could not call getLastInputInfo", "")
	return time.Duration((uint32(currentTickCount) - lastInputInfo.dwTime)) * time.Millisecond
}

// checks whether the windows desktop is currently locked by looking if LogonUI.exe is currently running
func screenIsLocked(errorsFromSyscalls *atomic.Value) bool {
	ps, err := processes()
	if err == nil {
		errorsFromSyscalls.CompareAndSwap("could not get desktop locked status", "")
		wp := findProcessByName(ps, "LogonUI.exe")
		if wp != nil {
			return true
		}
	} else {
		errorsFromSyscalls.CompareAndSwap("", "could not get desktop locked status")
		return false
	}
	return false
}

// formats the time worked into a string of format "hh:mm"
func formatSecondsForClipboard(wTimer *WorkTimer) string {
	seconds := wTimer.GetPreliminaryWorkTimeSeconds()
	hoursWorked := seconds / 3600
	minutesWorked := (seconds - hoursWorked*3600) / 60
	hoursText := ""
	if hoursWorked < 10 {
		hoursText = "0" + strconv.Itoa(int(hoursWorked))
	} else {
		hoursText = strconv.Itoa(int(hoursWorked))
	}
	minutesText := ""
	if minutesWorked < 10 {
		minutesText = "0" + strconv.Itoa(int(minutesWorked))
	} else {
		minutesText = strconv.Itoa(int(minutesWorked))
	}
	return hoursText + ":" + minutesText
}

// https://stackoverflow.com/a/53572104
// helper functions to read windows process IDs and names in order to determin if LogonUI.exe is running
const TH32CS_SNAPPROCESS = 0x00000002

// struct used by newWindowsProcess
type WindowsProcess struct {
	ProcessID       int
	ParentProcessID int
	Exe             string
}

// decodes a process to retrieve its PID and EXE-name
func newWindowsProcess(e *syscall.ProcessEntry32) WindowsProcess {
	// Find when the string ends for decoding
	end := 0
	for {
		if e.ExeFile[end] == 0 {
			break
		}
		end++
	}

	return WindowsProcess{
		ProcessID:       int(e.ProcessID),
		ParentProcessID: int(e.ParentProcessID),
		Exe:             syscall.UTF16ToString(e.ExeFile[:end]),
	}
}

// take a snapshot of all running processes
func processes() ([]WindowsProcess, error) {
	handle, err := syscall.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(handle)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	// get the first process
	err = syscall.Process32First(handle, &entry)
	if err != nil {
		return nil, err
	}

	results := make([]WindowsProcess, 0, 50)
	for {
		results = append(results, newWindowsProcess(&entry))

		err = syscall.Process32Next(handle, &entry)
		if err != nil {
			// windows sends ERROR_NO_MORE_FILES on last process
			if err == syscall.ERROR_NO_MORE_FILES {
				return results, nil
			}
			return nil, err
		}
	}
}

// find a process by name instead of its PID
func findProcessByName(processes []WindowsProcess, name string) *WindowsProcess {
	for _, p := range processes {
		if bytes.Contains([]byte(strings.ToUpper(p.Exe)), []byte(strings.ToUpper(name))) {
			return &p
		}
	}
	return nil
}
