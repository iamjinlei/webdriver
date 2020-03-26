package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/iamjinlei/webdriver"
)

func main() {
	snap := flag.Bool("snap", false, "take snap of the page")
	flag.Parse()

	if err := webdriver.Init(9090, false); err != nil {
		fmt.Printf("init error %v\n", err)
		return
	}
	defer webdriver.Shutdown()

	s, err := webdriver.New("", 1920, 1080, false, time.Minute)
	if err != nil {
		fmt.Printf("error creating a new session %v\n", err)
		return
	}

	if err := s.Get("http://www.beijing-time.org/"); err != nil {
		fmt.Printf("error loading page %v\n", err)
		return
	}

	container, err := s.GetDOM("//div[contains(@class, 'container')]")
	if err != nil {
		fmt.Printf("error locating container class %v\n", err)
		return
	}

	links, err := container.GetDOM("//div[@class='comment-list comment-parent comment-view']")
	if err != nil {
		fmt.Printf("error locating links %v\n", err)
		return
	}

	cls, err := links.GetAttribute("class")
	if err != nil {
		fmt.Printf("error readinng class attribute %v\n", err)
		return
	}
	if "comment-list comment-parent comment-view" != cls {
		fmt.Printf("unexpected class attribute value %v\n", cls)
		return
	}

	items, err := s.GetDOMs("//a[@class='nav-item']")
	if err != nil {
		fmt.Printf("error locatinng nav items %v\n", err)
		return
	}
	if len(items) != 3 {
		fmt.Printf("unexpected # of nav items %v\n", len(items))
		return
	}
	if err := s.ClickDOM("//a[@class='nav-item'][2]"); err != nil {
		fmt.Printf("error clicking nav item #3 %v\n", err)
		return
	}

	cform, err := s.GetDOM("//form[@id='cform']")
	if err != nil {
		fmt.Printf("error locating cform DOM %v\n", err)
		return
	}

	if *snap {
		cform.Snap()
	}
}
