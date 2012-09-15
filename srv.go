package rbot

import (
	"code.google.com/p/goweb/goweb"
	"fmt"
	//	"launchpad.net/mgo"
)

func main() {

	goweb.MapFunc("/people/{name}/animals/{animal_name}", func(c *goweb.Context) {
		fmt.Fprintf(c.ResponseWriter,
			"Hey %s, your favourite animal is a %s",
			c.PathParams["name"],
			c.PathParams["animal_name"])
	})
	goweb.ListenAndServe(":8080")

}
