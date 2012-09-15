package rbot

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io/ioutil"
	"launchpad.net/mgo"
	"launchpad.net/mgo/bson"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type rbot struct {
	config             map[string]string
	session            *mgo.Session
	articlescollection *mgo.Collection
	client             *http.Client
	logger             *log.Logger
}

type LoginReply struct {
	Json struct {
		Errors []string
		Data   struct {
			Modhash string
			Cookie  string
		}
	}
}

type Entry struct {
	Link []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
	Title  string `xml:"title"`
	Id     string `xml:"id"`
	Source struct {
		Title string `xml:"title"`
	} `xml:"source"`
	State string
}

type Feed struct {
	Entries []Entry `xml:"entry"`
}

func (b rbot) FetchAtomFeed() (feed Feed, err error) {
	var r *http.Response
	r, err = http.Get(b.config["feedurl"])
	if err != nil {
		return feed, err
	}
	defer r.Body.Close()
	if r.StatusCode == http.StatusOK {
		decoder := xml.NewDecoder(r.Body)
		decoder.Decode(&feed)
	}
	return feed, nil
}

func (b rbot) StoreNewEntries(entries []Entry) (newarticles int, err error) {
	for i := 0; i < len(entries); i++ {
		entries[i].State = "pending"
		var c int
		c, err = b.articlescollection.Find(bson.M{"id": entries[i].Id}).Count()
		if err != nil {
			return 0, nil
		}
		if c == 0 {
			err = b.articlescollection.Insert(entries[i])
			if err != nil {
				return 0, err
			}
			newarticles++
		}
	}
	return newarticles, nil
}

func (b rbot) post(url string, postparams url.Values) (r *http.Response, err error) {
	request, err := http.NewRequest("POST", url, strings.NewReader(postparams.Encode()))
	if err != nil {
		return nil, err
	}
	defer request.Body.Close()

	for _, cookie := range b.client.Jar.Cookies(request.URL) {
		request.AddCookie(cookie)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", b.config["useragent"])

	response, responseerr := b.client.Do(request)

	if responseerr == nil && b.client.Jar != nil {
		b.client.Jar.SetCookies(request.URL, response.Cookies())
	}

	return response, responseerr
}

func (b rbot) PostOneNewArticle() (posted, queued int, err error) {
	query := b.articlescollection.Find(bson.M{"state": "pending"})
	queued, err = query.Count()
	if err != nil {
		return 0, 0, err
	}
	if queued > 0 {
		var a Entry
		err := query.One(&a)
		if err != nil {
			return queued, 0, err
		}
		result, err := b.PostArticle(a)
		if err != nil {
			return queued, 0, err
		}
		posted = 1
		b.articlescollection.Update(bson.M{"id": a.Id}, bson.M{"$set": bson.M{"result": result, "state": "attempted"}})
	}
	return posted, queued, nil
}

func (b rbot) PostArticle(entry Entry) (postreply map[string]interface{}, err error) {
	loginresp, err := b.post(
		b.config["redditloginurl"],
		url.Values{
			"api_type": {"json"},
			"user":     {b.config["reddituser"]},
			"passwd":   {b.config["redditpassword"]},
		},
	)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(loginresp.Body)
	if err != nil {
		return nil, err
	}

	var loginreply *LoginReply
	json.Unmarshal(body, &loginreply)
	loginresp.Body.Close()

	var href string = ""
	for _, link := range entry.Link {
		if link.Rel == "canonical" {
			href = link.Href
		}
		if link.Rel == "alternate" && href == "" {
			href = link.Href
		}
	}

	postresp, err := b.post(
		b.config["redditsubmiturl"],
		url.Values{
			"api_type": {"json"},
			"kind":     {"link"},
			"title":    {html.UnescapeString(entry.Title)},
			"url":      {href},
			"sr":       {b.config["redditsubreddit"]},
			"r":        {b.config["redditsubreddit"]},
			"uh":       {loginreply.Json.Data.Modhash},
		},
	)
	if err != nil {
		return nil, err
	}

	body, err = ioutil.ReadAll(postresp.Body)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(body, &postreply)
	postresp.Body.Close()
	return postreply, nil
}

type cjar struct {
	cookies []*http.Cookie
}

func (cj *cjar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	cj.cookies = cookies
}

func (cj *cjar) Cookies(u *url.URL) []*http.Cookie {
	return cj.cookies
}

func newrbot(session *mgo.Session, dbname string, logger *log.Logger) (b rbot) {

	b.session = session
	b.config = make(map[string]string)

	b.logger = logger

	configcollection := b.session.DB(dbname).C("config")
	iter := configcollection.Find(nil).Iter()
	result := struct {
		Name  string
		Value string
	}{}
	for iter.Next(&result) {
		b.config[result.Name] = result.Value
		if result.Name != "redditpassword" {
			b.logger.Printf("%s -> %s\n", result.Name, result.Value)
		}
	}

	b.articlescollection = b.session.DB(dbname).C("articles")

	redditloginurl := bytes.NewBufferString("")
	fmt.Fprintf(redditloginurl, "%s/%s", b.config["redditbaseloginurl"], b.config["reddituser"])
	b.config["redditloginurl"] = string(redditloginurl.Bytes())

	useragent := bytes.NewBufferString("")
	fmt.Fprintf(useragent, "rbot.go/%s", b.config["reddituser"])
	b.config["useragent"] = string(useragent.Bytes())

	b.client = &http.Client{
		Jar: &cjar{},
	}

	return b
}

func Run(logger *log.Logger, dbserver, dbname string) (err error) {

	session, err := mgo.Dial(os.Args[1])
	if err != nil {
		logger.Printf("error: can't connect to mongodb server @ %s", os.Args[1])
		return err
	}

	b := newrbot(session, os.Args[2], logger)

	freq, err := strconv.ParseInt(b.config["frequency"], 10, 0)
	if err != nil {
		logger.Printf("error: can't parse frequency from the config db\n")
		return (err)
	}

	if freq < 1 {
		freq = 60
		b.logger.Printf("using a default value of %d seconds for the frequency\n",
			freq)
	} else {
		b.logger.Printf("using a value of %d seconds for the frequency\n",
			freq)
	}

	for {

		feed, err := b.FetchAtomFeed()

		if err == nil {

			newarticlecount, err := b.StoreNewEntries(feed.Entries)
			if err != nil {
				b.logger.Printf("failed to StoreNewEntries (%s)\n", err)
				return err
			}

			postedcount, queuedcount, err := b.PostOneNewArticle()

			if err != nil {
				b.logger.Printf("failed to PostOneNewArticle (%s)\n", err)
				return err
			}

			b.logger.Printf("new: %d, posted: %d, queued: %d\n",
				newarticlecount, postedcount, queuedcount)

		} else {

			b.logger.Printf("error fetching atom feed (%s)\n", err)

		}

		time.Sleep(time.Duration(freq) * time.Second)

	}
	return nil

}
