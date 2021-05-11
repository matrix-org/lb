### Mobile bindings

This contains a simple API which is capable of being automatically converted into mobile libraries (e.g Java).
For Android:
```
gomobile bind -target=android
```
Then copy `mobile-sources.jar` and `mobile.aar` to a suitable Android project.

There are many connection parameters which can be configured, and it is important developers understand what
they do. There are sensible defaults, but this is only sensible for Element clients running over the public
internet. If you are running in a different network environment or with a different client, there may be
better configurations. The parameters are well explained in the code, along with the trade-offs of setting
them too high/low.