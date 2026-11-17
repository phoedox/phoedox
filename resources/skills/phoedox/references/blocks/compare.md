# ```compare – two versions side by side

Shows two versions of a file or setting side by side with the differences marked.
`--- title` starts the old version, `+++ title` the new one.

````markdown
```compare
--- router.conf (before)
address 192.168.1.1/24
+++ router.conf (after)
address 192.168.10.1/24
mtu 9000
```
````

Published pages show it too.
