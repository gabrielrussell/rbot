package main

import (
  "bytes"
  "encoding/json"
  "encoding/xml"
  "fmt"
  "html"
  "io/ioutil"
  "launchpad.net/mgo"
  "launchpad.net/mgo/bson"
  "net/http"
  "net/url"
  "os"
  "strings"
)

type rbot struct {
  dbname             string
  useragent          string
  reddituser         string
  redditpassword     string
  redditsubmiturl    string
  redditbaseloginurl string
  redditloginurl     string
  redditsubreddit    string
  feedurl            string
  session            *mgo.Session
  articlescollection *mgo.Collection
  configcollection   *mgo.Collection
  client             *http.Client
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
  Title string `xml:"title"`
  Id    string `xml:"id"`
  State string
}

type Feed struct {
  Entries []Entry `xml:"entry"`
}

func (b rbot) FetchAtomFeed() (feed Feed, err error) {
  var r *http.Response
  r, err = http.Get(b.feedurl)
  if err != nil {
    return feed, err
  }
  if r.StatusCode == http.StatusOK {
    decoder := xml.NewDecoder(r.Body)
    decoder.Decode(&feed)
  }
  return feed, nil
}

func (b rbot) StoreNewEntries(entries []Entry) {
  var err error
  for i := 0; i < len(entries); i++ {
    entries[i].State = "pending"
    var c int
    c, err = b.articlescollection.Find(bson.M{"id": entries[i].Id}).Count()
    if err != nil {
      panic(err)
    }
    if c == 0 {
      err = b.articlescollection.Insert(entries[i])
      if err != nil {
        panic(err)
      }
    }
  }
}

func (b rbot) post(url string, postparams url.Values) (r *http.Response, err error) {

  request, err := http.NewRequest("POST", url, strings.NewReader(postparams.Encode()))
  if err != nil {
    return nil, err
  }

  for _, cookie := range b.client.Jar.Cookies(request.URL) {
    request.AddCookie(cookie)
  }
  request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
  request.Header.Set("User-Agent", b.useragent)

  response, responseerr := b.client.Do(request)

  if responseerr == nil && b.client.Jar != nil {
    b.client.Jar.SetCookies(request.URL, response.Cookies())
  }

  return response, responseerr
}

func (b rbot) PostOneNewArticle() {
  query := b.articlescollection.Find(bson.M{"state": "pending"})
  count, err := query.Count()
  if err != nil {
    panic(err)
  }
  if count > 0 {
    var a Entry
    err := query.One(&a)
    if err != nil {
      panic(err)
    }
    result := b.PostArticle(a)
    b.articlescollection.Update(bson.M{"id": a.Id}, bson.M{"$set": bson.M{"result": result, "state": "attempted"}})
  }
}

func (b rbot) PostArticle(entry Entry) map[string]interface{} {

  loginresp, err := b.post(
    b.redditloginurl,
    url.Values{
      "api_type": {"json"},
      "user":     {b.reddituser},
      "passwd":   {b.redditpassword},
    },
  )
  if err != nil {
    panic(err)
  }

  body, err := ioutil.ReadAll(loginresp.Body)
  if err != nil {
    panic(err)
  }

  var loginreply *LoginReply
  json.Unmarshal(body, &loginreply)
  loginresp.Body.Close()

  postresp, err := b.post(
    b.redditsubmiturl,
    url.Values{
      "api_type": {"json"},
      "kind":     {"link"},
      "title":    {html.UnescapeString(entry.Title)},
      "url":      {entry.Link[0].Href},
      "sr":       {b.redditsubreddit},
      "r":        {b.redditsubreddit},
      "uh":       {loginreply.Json.Data.Modhash},
    },
  )
  if err != nil {
    panic(err)
  }

  body, err = ioutil.ReadAll(postresp.Body)
  if err != nil {
    panic(err)
  }
  var postreply map[string]interface{}
  json.Unmarshal(body, &postreply)
  postresp.Body.Close()
  return postreply
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

func (b rbot) config(name string) (value string) {
  var namevalue struct {
    Name  string
    Value string
  }
  err := b.configcollection.Find(bson.M{"name": name}).One(&namevalue)
  if err != nil {
    fmt.Printf("%s\nconfig value %s not found\n", err, name)
    os.Exit(1)
  }
  return namevalue.Value
}

func newrbot(servername string, dbname string) (b rbot, err error) {
  b.session, err = mgo.Dial(servername)
  if err != nil {
    return b, err
  }

  b.dbname = dbname

  b.configcollection = b.session.DB(b.dbname).C("config")

  b.articlescollection = b.session.DB(b.dbname).C("articles")

  b.reddituser = b.config("reddituser")
  b.redditpassword = b.config("redditpassword")
  b.feedurl = b.config("feedurl")
  b.redditsubmiturl = b.config("redditsubmiturl")
  b.redditbaseloginurl = b.config("redditbaseloginurl")
  b.redditsubreddit = b.config("redditsubreddit")

  redditloginurl := bytes.NewBufferString("")
  fmt.Fprintf(redditloginurl, "%s/%s", b.redditbaseloginurl, b.reddituser)
  b.redditloginurl = string(redditloginurl.Bytes())

  useragent := bytes.NewBufferString("")
  fmt.Fprintf(useragent, "rbot.go/%s", b.reddituser)
  b.useragent = string(useragent.Bytes())

  b.client = &http.Client{
    Jar: &cjar{},
  }

  return b, nil
}

func (b rbot) destroy() {
  b.session.Close()
}

func main() {
  if len(os.Args) < 3 {
    fmt.Printf("usage: %s <mongodb-server> <db-name>", os.Args[0])
    os.Exit(1)
  }

  b, err := newrbot(os.Args[1], os.Args[2])
  defer b.destroy()

  feed, err := b.FetchAtomFeed()
  if err != nil {
    panic(err)
  }

  b.StoreNewEntries(feed.Entries)

  b.PostOneNewArticle()

}
