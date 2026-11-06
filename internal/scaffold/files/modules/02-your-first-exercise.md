---
description: Download the starter files, finish the function and compare with the solution.
part: Getting started
duration: 20 min
---

# Your First Exercise

A directory in `exercises/` named like a module becomes that module's exercise:
the box above is generated from `exercises/02-your-first-exercise/`.

## The task

Open `greet.py` and make `greet` return a greeting. You can show real files in a
module instead of copying them, so the text can't drift from the code:

<!-- include: exercises/02-your-first-exercise/greet.py lines=1-3 -->

Then run it:

```bash
python3 greet.py
```

> [!HINT]
> Python's f-strings put a variable in a string: `f"Hi {name}"`.

> [!SOLUTION]
> ```python
> def greet(name):
>     return f"Hello, {name}!"
> ```
>
> The `solution/` directory is left out of the starter download, and has a
> download of its own.

## What's next

You've seen everything a module can do. Add your own with:

```bash
wsbuilder new "My next module" --exercise
```
