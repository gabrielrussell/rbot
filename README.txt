RBot, a bot for reposting articles from an rss feed in to a reddit subreddit

To compile, I just:
  
  go build rbot.go

Edit the config.json file and add the values from feedurl, reddituser, redditsubreddit, and redditpassword. Then run:

  mongoimport --db <db-name> -c config --drop config.json

Then you can run the rbot with:

  rbot <mongo-server> <db-name>

It will grab all of the articles from the feed, but it will only post one of them every time you run rbot.

Then you can run it under cron in a loop to look for new articles and post one new article every five minutes, for example.
