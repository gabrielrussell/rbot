RBot, a bot for reposting articles from an rss feed in to a reddit subreddit

To compile, I just:
  
  go build rbot.go

Edit the config.json file and add the values from feedurl, reddituser, redditsubreddit, and redditpassword. Then run:

  mongoimport --db <db-name> -c config --drop config.json

Then you can run the rbot with:

  rbot <mongo-server> <db-name>

Every config["frequency"] seconds it will grab all of the new articles from the feed, store them in the mongodb store, and post at most one unposted article.
