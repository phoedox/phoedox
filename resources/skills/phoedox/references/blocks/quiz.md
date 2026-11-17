# ```quiz – quizzes and flashcards

`? question`, then `+ right` and `- wrong` answers (several `+` allowed) or `= answer` for
a flashcard, optionally `> explanation`. Each reader's answers are their own and are kept.
Good for onboarding ("Do you know our network?").

````markdown
```quiz
title: Do you know our network?
? Which address does the router have?
- 192.168.1.1
+ 192.168.10.1
> Since the rebuild in March the home network is 192.168.10.0/24.

? What is the guest Wi-Fi called?
= Garden house
```
````

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. Quizzes are for pages only (not on whiteboards).
