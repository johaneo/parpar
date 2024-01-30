JSON Parsing and quickly
========================

## Context

This is retroactive readme; I've not worked on this code for a while. But the ideas may be generally interesting, and in case you don't want to read old code, 
this is a useful overview. Some of this overview is aspirational: I am not sure how gracefully the implementation handles the "Is it actually parsing" cases.

## TL;DR

This aims to provide a library to parse large json documents quickly. 
We use two main tricks.

1. we will parse the file in parallel in multiple go-routines (and thus cores)
2. we will not return a fully populated dictionary representation, but rather a skeleton that can be navigated to extract values or sub-documents.

If you want to process small documents or want the whole document rendered into an in-memory representation, the default parser is well suited. 

# Parallel parsing

In truth, this project came about when I wondered whether we could speed up parsing in general by parallelizing it. 
This is hard to do, given that parsing is quite context sensitive[1]: whether the fragment ... `foo(bar` ... is a function call or definition depends on where it occurs. 

But not for Json. There is precious little context in json: The only context is whether a fragment ... ` [1 ,2, 3] ` ... occurs inside or outside the quotes of a string literal. 
We can scan for all json tokens (`[]"{},:` from memory) in parallel, producing several lists of tokens, that we can then process in order.

So Json became the test-bed.

# Structural strategy

We split the input into chunks, and process each `start,end` chunk independently by a goroutine. Each chunk is processed into slice of `token, global-offset` structs.
Chunks are sorted into input order, matching open-close quotation marks are identified, and any tokens inbetween and inclusive are removed from the token slices.[2]
Lastly, we create the parse tree by matching open-close dictionary and lists, and next item with those as well. 

As a visual aid, you can imagine that this input

```
{
  "alist": [1 ,2, 3],
  "adict": { "a": "{look curly}" }
}
```

is split into 

```
{
  "alist": [1 ,2,
```
,
``` 3],
  "adict": { "a": "{look
```
,
```
curly}" }
}
```



# Notes 
[1] ironically, the formal category of many language definitions is Context *Free* Grammar.
[2] efficient slice manipulation is probably the lowest hanging fruit currently. 
    I suspect it would be best to *not* change the slices at all, but instead introduce another layer ontop of "active ranges" within each slice. 
