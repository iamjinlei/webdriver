package webdriver

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/phayes/freeport"
	"github.com/pkg/errors"

	"github.com/iamjinlei/memfs"
)

var (
	ErrWaitTimeout        = errors.New("wait timed out")
	ErrNeedRetry          = errors.New("need retry")
	ErrUnknown            = errors.New("unknown error")
	ErrNotFound           = errors.New("element not found")
	ErrInvaidSelectorPath = errors.New("invalid selector path")
)

var debugFlag = false

// SetDebug sets debug mode
func SetDebug(debug bool) {
	debugFlag = debug
}

type driver struct {
	port            int
	addr            string
	cmd             *exec.Cmd
	shutdownURLPath string
}

func (d *driver) Stop() error {
	// Selenium 3 stopped supporting the shutdown URL by default.
	// https://github.com/SeleniumHQ/selenium/issues/2852
	if d.shutdownURLPath == "" {
		if err := d.cmd.Process.Kill(); err != nil {
			return err
		}
	} else {
		resp, err := http.Get(d.addr + d.shutdownURLPath)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}

	if err := d.cmd.Wait(); err != nil && err.Error() != "signal: killed" {
		return err
	}

	return nil
}

type server struct {
	d         *driver
	port      int
	ownDriver bool
}

var inst *server
var sessions []*Session
var smu sync.Mutex

func Init(port int, debug bool) error {
	chromeDriverPath := strings.TrimSpace(os.Getenv("CHROME_DRIVER"))
	if chromeDriverPath == "" {
		return fmt.Errorf("env CHROME_DRIVER is missing")
	}

	if port < 1000 {
		return fmt.Errorf("driver port < 1000: %v", port)
	}

	// detect chrome driver running process
	out, _ := exec.Command("pgrep", filepath.Base(chromeDriverPath)).CombinedOutput()
	pidStr := strings.TrimSpace(string(out))
	if len(pidStr) > 0 {
		fmt.Printf("*** [webdriver] detected chrome driver running process (PIDs = %v) ***\n", strings.Replace(pidStr, "\n", ", ", -1))
	}

	smu.Lock()
	defer smu.Unlock()
	if inst != nil {
		return nil
	}

	SetDebug(debug)

	d, isOwned, err := newChromeDriver(chromeDriverPath, port)
	if err != nil {
		return err
	}

	inst = &server{
		d:         d,
		port:      port,
		ownDriver: isOwned,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Printf("*** [webdriver] interrupt signal received ***\n")
			Shutdown()
			os.Exit(0)
		}
	}()

	return nil
}

func newChromeDriver(path string, port int) (*driver, bool, error) {
	d := &driver{
		port:            port,
		addr:            fmt.Sprintf("http://localhost:%d/wd/hub", port),
		shutdownURLPath: "/shutdown",
		cmd:             exec.Command(path, "--port="+strconv.Itoa(port), "--url-base=wd/hub", "--verbose"),
	}

	if debugFlag {
		d.cmd.Stderr = os.Stderr
		d.cmd.Stdout = os.Stdout
	}
	d.cmd.Env = os.Environ()

	status := func(addr string) int {
		resp, err := http.Get(addr + "/status")
		if err == nil {
			resp.Body.Close()
			switch resp.StatusCode {
			// Selenium <3 returned Forbidden and BadRequest. ChromeDriver and
			// Selenium 3 return OK.
			case http.StatusForbidden, http.StatusBadRequest, http.StatusOK:
				return http.StatusOK
			default:
				return resp.StatusCode
			}
		}

		return http.StatusInternalServerError
	}

	if status(d.addr) == http.StatusOK {
		return d, false, nil
	}

	fmt.Printf("*** [webdriver] starting chromedriver ***\n")
	if err := d.cmd.Start(); err != nil {
		return nil, false, err
	}

	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if status(d.addr) == http.StatusOK {
			return d, true, nil
		}
	}

	return nil, false, fmt.Errorf("failed to start chrome driver on port %d", port)
}

func Shutdown() {
	smu.Lock()
	defer smu.Unlock()
	for _, s := range sessions {
		fmt.Printf("*** [webdriver] closing session %v ***\n", s.SessionID())
		s.Quit()
	}
	sessions = nil

	if inst != nil && inst.ownDriver {
		fmt.Printf("*** [webdriver] stopping webdriver ***\n")
		inst.d.Stop()
		inst = nil
	} else {
		fmt.Printf("*** [webdriver] leave alone webdriver (not owned) ***\n")
	}
	fmt.Printf("*** [webdriver] shutdown complete ***\n\n")
}

type Session struct {
	WebDriver
	timeout time.Duration
}

type Element struct {
	s *Session
	WebElement
}

func New(profile string, w, h int, headless bool, timeout time.Duration) (*Session, error) {
	caps := Capabilities{"browserName": "chrome"}

	chromeCfg := chromeCapabilities{
		Args: []string{
			fmt.Sprintf("window-size=%v,%v", w, h),
			"disable-notifications",
		},
	}
	if headless {
		chromeCfg.Args = append(chromeCfg.Args, "headless")
	}
	if profile != "" {
		chromeCfg.Args = append(chromeCfg.Args, fmt.Sprintf("user-data-dir=%v", profile))
	}

	caps.AddChrome(chromeCfg)

	d, err := NewRemote(caps, fmt.Sprintf("http://localhost:%d/wd/hub", inst.port))
	if err != nil {
		return nil, err
	}

	s := &Session{d, timeout}

	smu.Lock()
	defer smu.Unlock()
	sessions = append(sessions, s)

	return s, nil
}

func (s *Session) Close() error {
	smu.Lock()
	idx := 0
	for ; idx < len(sessions); idx++ {
		if s == sessions[idx] {
			break
		}
	}
	sessions[idx] = sessions[len(sessions)-1]
	sessions = sessions[:len(sessions)-1]
	smu.Unlock()

	return s.Quit()
}

func (s *Session) find(xpath string) (*Element, error) {
	elem, err := s.FindElement(ByXPATH, xpath)
	if notFound(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	} else if elem == nil {
		return nil, ErrNotFound
	}

	return &Element{s, elem}, nil
}

func (s *Session) findN(xpath string) ([]*Element, error) {
	elements, err := s.FindElements(ByXPATH, xpath)
	if notFound(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	} else if len(elements) == 0 {
		return nil, ErrNotFound
	}

	ret := []*Element{}
	for _, elem := range elements {
		ret = append(ret, &Element{s, elem})
	}
	return ret, nil
}

// GetDOM expects the element existence
func (s *Session) GetDOM(xpath string) (*Element, error) {
	return s.GetDOMTimeout(xpath, s.timeout)
}

func (s *Session) GetDOMTimeout(xpath string, to time.Duration) (*Element, error) {
	var ret *Element
	err := waitOn(func() (bool, error) {
		elem, err := s.find(xpath)
		if err == ErrNotFound {
			return false, nil
		} else if err != nil {
			return true, err
		} else if elem == nil {
			// Should this happen?
			return false, nil
		}

		ret = elem
		return true, nil
	}, to)

	return ret, err
}

// GetDOMs expects elements existence
func (s *Session) GetDOMs(xpath string) ([]*Element, error) {
	var ret []*Element
	err := waitOn(func() (bool, error) {
		elems, err := s.findN(xpath)
		if err == ErrNotFound {
			return false, nil
		} else if err != nil {
			return true, err
		} else if len(elems) == 0 {
			// Should this happen?
			return false, nil
		}

		ret = elems
		return true, nil
	}, s.timeout)

	return ret, err
}

func (s *Session) ClickDOM(xpath string) error {
	return waitOn(func() (bool, error) {
		elem, err := s.find(xpath)
		if err == ErrNotFound {
			return false, nil
		} else if err != nil {
			return true, err
		}

		if err := elem.ScrollIntoView(); err != nil {
			return true, err
		}

		if err := elem.Click(); err != nil {
			return true, err
		}

		return true, nil
	}, s.timeout)
}

func (e *Element) find(xpath string) (*Element, error) {
	elem, err := e.WebElement.FindElement(ByXPATH, xpath)
	if notFound(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	} else if elem == nil {
		return nil, ErrNotFound
	}
	return &Element{e.s, elem}, nil
}

func (e *Element) findN(xpath string) ([]*Element, error) {
	elements, err := e.WebElement.FindElements(ByXPATH, xpath)
	if notFound(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	} else if len(elements) == 0 {
		return nil, ErrNotFound
	}

	ret := []*Element{}
	for _, elem := range elements {
		ret = append(ret, &Element{e.s, elem})
	}
	return ret, nil
}

// GetDOM expects the element existence
func (e *Element) GetDOM(xpath string) (*Element, error) {
	var ret *Element
	err := waitOn(func() (bool, error) {
		elem, err := e.find(xpath)
		if err == ErrNotFound {
			return false, nil
		} else if err != nil {
			return true, err
		} else if elem == nil {
			// Should this happen?
			return false, nil
		}

		ret = elem
		return true, nil
	}, e.s.timeout)

	return ret, err
}

// GetDOMs expects elements existence
func (e *Element) GetDOMs(xpath string) ([]*Element, error) {
	var ret []*Element
	err := waitOn(func() (bool, error) {
		elems, err := e.findN(xpath)
		if err == ErrNotFound {
			return false, nil
		} else if err != nil {
			return true, err
		} else if len(elems) == 0 {
			// Should this happen?
			return false, nil
		}

		ret = elems
		return true, nil
	}, e.s.timeout)

	return ret, err
}

func (e *Element) ClickDOM(xpath string) error {
	return waitOn(func() (bool, error) {
		elem, err := e.find(xpath)
		if err == ErrNotFound {
			return false, nil
		} else if err != nil {
			return true, err
		}

		if err := elem.ScrollIntoView(); err != nil {
			return true, err
		}

		if err := elem.Click(); err != nil {
			return true, err
		}

		return true, nil
	}, e.s.timeout)
}

func notFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such element")
}

func StaleElement(err error) bool {
	return err != nil && strings.Contains(err.Error(), "stale element reference")
}

func (s *Session) Wait(xpaths []string) (int, error) {
	selected := -1
	err := waitOn(func() (bool, error) {
		status, err := s.Status()
		if err != nil {
			return true, err
		}
		if !status.Ready {
			return false, nil
		}

		for idx, xpath := range xpaths {
			result, err := s.find(xpath)
			if err == ErrNotFound {
				continue
			} else if err != nil {
				return true, err
			} else if result != nil {
				selected = idx
				return true, nil
			}
		}

		return false, nil
	}, s.timeout)

	return selected, err
}

func (e *Element) Wait(xpaths []string) (int, error) {
	selected := -1
	err := waitOn(func() (bool, error) {
		status, err := e.s.Status()
		if err != nil {
			return true, err
		}
		if !status.Ready {
			return false, nil
		}

		for idx, xpath := range xpaths {
			result, err := e.find(xpath)
			if err == ErrNotFound {
				continue
			} else if err != nil {
				return true, err
			} else if result != nil {
				selected = idx
				return true, nil
			}
		}

		return false, nil
	}, e.s.timeout)

	return selected, err
}

func (s *Session) Snap() error {
	img, err := s.Screenshot()
	if err != nil {
		return err
	}

	return serveSnap(img)
}

func (e *Element) Txt() string {
	if txt, err := e.Text(); err != nil {
		return ""
	} else {
		return strings.TrimSpace(txt)
	}
}

func (e *Element) Parent() (*Element, error) {
	parent, err := e.WebElement.FindElement(ByXPATH, "..")
	if err != nil {
		return nil, err
	}

	return &Element{e.s, parent}, nil
}

func (e *Element) SetAttribute(attr, val string) error {
	_, err := e.s.ExecuteScript("arguments[0].setAttribute(arguments[1], arguments[2]);", []interface{}{e.WebElement, attr, val})
	return err
}

func (e *Element) ScrollIntoView() error {
	if _, err := e.s.ExecuteScript("arguments[0].scrollIntoView({behavior: \"auto\", block: \"center\", inline: \"center\"});", []interface{}{e.WebElement}); err != nil {
		return err
	}

	return waitOn(func() (bool, error) {
		if displayed, err := e.WebElement.IsDisplayed(); err != nil {
			return true, err
		} else if displayed {
			return true, nil
		}
		return false, nil
	}, e.s.timeout)
}

func (e *Element) Snap() error {
	return e.s.Snap()
}

func (s *Session) NoStale(fn func() error) error {
	return waitOn(func() (bool, error) {
		err := fn()
		if err == ErrNeedRetry || StaleElement(err) {
			return false, nil
		}
		return true, err
	}, s.timeout)
}

func waitOn(fn func() (bool, error), timeout time.Duration) error {
	ticker := time.NewTicker(1000 * time.Millisecond)
	to := time.NewTimer(timeout)
	for {
		select {
		case <-ticker.C:
			if done, err := fn(); err != nil {
				return err
			} else if done {
				return nil
			}

		case <-to.C:
			return errors.Wrapf(ErrWaitTimeout, string(debug.Stack()))
		}
	}

	return ErrUnknown
}

func serveSnap(img []byte) error {
	fsMap := map[string][]byte{
		"/snap.png": img,
		"/index.html": []byte(`
<!doctype html>
<html>
	<head>
		<title>Selenium debug snapshot</title>
		<link rel="icon" href="data:;base64,iVBORw0KGgo=">
	</head>
	<body>
		<img src="snap.png" style="width:800px" alt="snap.png">
	</body>
</html>
`),
	}

	var wg sync.WaitGroup
	wg.Add(3)

	fs, err := memfs.New(fsMap, map[string]func(path string){
		"Close": func(path string) {
			fmt.Printf("close %v\n", path)
			wg.Done()
		},
	})
	if err != nil {
		return err
	}

	port, err := freeport.GetFreePort()
	if err != nil {
		return err
	}

	fmt.Printf("serving http://localhost:%v\n", port)

	mux := http.NewServeMux()
	mux.Handle("/", http.StripPrefix("/", http.FileServer(fs)))
	srv := &http.Server{Addr: fmt.Sprintf(":%v", port), Handler: mux}

	go func() {
		srv.ListenAndServe()
	}()

	wg.Wait()
	return srv.Shutdown(context.TODO())
}
