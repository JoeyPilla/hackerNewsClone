package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

type storyCache struct {
	numStories int
	cache1     []Result
	cache2     []Result
	useCache1  bool
	experation time.Time
	duration   time.Duration
	mutex      sync.Mutex
}

func (sc *storyCache) getCachedStories() []Result {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	if time.Now().Sub(sc.experation) < 0 {
		if sc.useCache1 {
			return sc.cache1
		} else {
			return sc.cache2
		}
	}
	stories := getItems(sc.numStories)
	sc.experation = time.Now().Add(sc.duration)
	sc.cache1 = stories
	return sc.cache1
}

func (sc *storyCache) Refresh(refreshChan <-chan time.Time) {
	for {
		<-refreshChan
		stories := getItems(sc.numStories)
		sc.mutex.Lock()
		if sc.useCache1 {
			sc.cache2 = stories
		} else {
			sc.cache1 = stories
		}
		sc.useCache1 = !sc.useCache1
		sc.mutex.Unlock()
	}
}

type Result struct {
	Item Item
	Id   int
	Host string
	Err  error
}

type Item struct {
	By          string `json:"by"`
	Descendants int    `json:"descendants"`
	ID          int    `json:"id"`
	Kids        []int  `json:"kids"`
	Score       int    `json:"score"`
	Time        int    `json:"time"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	URL         string `json:"url"`
}

type Ret struct {
	Arr  []Result
	Time string
}

func main() {
	tmpl := template.Must(template.ParseFiles("index.tmpl"))
	http.HandleFunc("/", handler(30, 15*time.Minute, tmpl))
	log.Fatal(http.ListenAndServe(":4001", nil))
}

func handler(numStories int, duration time.Duration, tmpl *template.Template) http.HandlerFunc {
	sc := storyCache{
		numStories: numStories,
		duration:   duration,
		useCache1:  true,
	}
	sc.getCachedStories()
	refreshChan := time.Tick(duration * 3 / 4)
	go sc.Refresh(refreshChan)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		stories := sc.getCachedStories()
		RET := Ret{
			Arr:  stories,
			Time: fmt.Sprintf("Time taken to render: %s", time.Since(start)),
		}
		tmpl.Execute(w, RET)
	})
}

func getIDs() []int {
	resp, err := http.Get("https://hacker-news.firebaseio.com/v0/topstories.json")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	var ids []int
	json.Unmarshal(body, &ids)
	return ids
}

func getItems(numResults int) []Result {
	ids := getIDs()
	results := []Result{}
	newItems := numResults * 5 / 4
	oldItems := 0
	for len(results) < numResults {
		newResults := getTopStories(ids[oldItems:newItems])
		results = append(results, newResults...)
		oldItems, newItems = newItems, newItems+30-len(newResults)
	}
	return results[:numResults]
}

func getTopStories(ids []int) []Result {
	retItem := make(chan Result)
	results := []Result{}
	for i := 0; i < len(ids); i++ {
		go getItem(i, ids[i], retItem)
	}

	for i := 0; i < len(ids); i++ {
		result := <-retItem
		if result.Item.Type == "story" {
			u, _ := url.Parse(result.Item.URL)
			result.Host = u.Host
			results = append(results, result)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Id < results[j].Id
	})

	return results
}

func getItem(id, itemID int, res chan Result) {
	resp, err := http.Get(fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json?print=pretty", itemID))
	if err != nil {
		res <- Result{
			Err:  err,
			Id:   id,
			Item: Item{},
		}
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	item := Item{}
	json.Unmarshal(body, &item)
	res <- Result{
		Item: item,
		Id:   id,
		Err:  nil,
	}
}
