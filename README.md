# webdriver

This package is a chrome webdriver client that wraps essential APIs for browser automation. The code was originally forked from https://github.com/tebeka/selenium with a large amount of trimming and modification to simplify the usage and prevent webdriver process leakage. This package does not rely on selenium Java tool anymore. It rather interacts with chromedriver directly and manages lifecycle of the chromedriver process. Multiple webdriver instances (processes use this package) are supported.

## Usage

Download webdriver binary from https://chromedriver.chromium.org/downloads that compatible with your local chrome version.
Set environment variable CHOME_DRIVER to the path of the downloaded binary.

The package maintains a global instance of the webdriver process. So make sure calling webdriver.Init() once before any usage.

```golang
func main() {
  // start webdriver process and use port 9090
  if err := webdriver.Init(9090, false); err != nil {
    fmt.Printf("init error %v\n", err)
    return
  }
  defer webdriver.Shutdown()

  // create a new webdriver session to work with, using 1920x1080 window size
  s, _ := webdriver.New("chrome_profile", 1920, 1080, false, time.Minute)
  s.Get("https://github.com/")
  
  // locate DOM object using xpath
  container, _ := s.GetDOM("//div[contains(@class, 'container')]")
}
```

You can check example folder for how to use session to interact with chrome
```bash
go run example/session.go
```
