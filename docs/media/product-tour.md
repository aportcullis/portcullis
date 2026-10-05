# Portcullis product tour

[Back to the project](../../README.md) · [Try it locally](../operations/pg-alpha-quickstart.md)

## Request, review, execute once

Compose a request on its own page, including a title, optional explanation, SQL, and typed parameters. SQL formatting is automatic, reversible, and configurable. Save a draft or submit it to freeze the approval inputs. A distinct reviewer approves or rejects the request; the original requester executes an approved statement once.

![A requester submits SQL, a distinct reviewer approves it, and the requester executes the statement once](workflow.gif)

![Request composition with a title, explanatory body, SQL and typed parameters](request.png)

Click a request title to expand its current workflow beneath the list row. The graph uses authorized summary facts and marks unavailable history explicitly.

![A pending request with its inline Draft, Review, Ready and Execution graph](requests.png)

## Explore results

The execution summary labels recorded server time and affected rows. This interval includes DB connection/execution, result collection and snapshot storage; approval waiting, browser rendering and later paging/sorting are excluded. The original requester also sees the durable summary in completed request details.

Choose a column and direction to sort the complete cached snapshot using its declared data type. Numeric precision and timezone-aware timestamp ordering are preserved; equal values keep their original order and NULL values stay last. Restore the original query order at any time. Sorting and filtering do not execute SQL again.

![Paged typed results with exact large integers, decimal revenue and visible-page copy](results.png)

![Paging through results, sorting revenue, filtering a region, and preparing the complete snapshot as CSV](results.gif)

Switch between Table and Text without rerunning SQL. Copy visible rows includes headers and only the current sorted/filtered page, escaping formula-like text for spreadsheet paste. Clipboard denial stays inline and leaves Text/CSV available.

![Tab-separated Text view of the same bounded result page](results-text.png)

CSV exports the complete snapshot in original query order, independently of the current page, sort, or filter. Results expire after 15 minutes and may be evicted earlier under quota pressure. The GIF stops at the prepared download link; actual file saving and readback are covered by the browser validation gate.

All screenshots and GIFs show the real application with synthetic data. Alex is the requester and Sam the reviewer; the demo reviewer is provisioned through a fixture because user-management screens are not yet available.

## Connections, policies, and authentication

### Connections and policies

Register and test targets, label their environment, and archive connections. Policy settings control statement classes, review requirements, timeouts, row limits, and result sizes. Reads are enabled by default with one distinct reviewer; writes and DDL require explicit enablement.

![PostgreSQL connections labeled as development and production](connections.png)

![Read, Write, and DDL permissions, approval counts, and execution limits](policy.png)

### Review and authentication

Reviewers inspect the request context, SQL, target snapshot, and approval status before deciding. Requests and results support direct links, reload, and browser history.
First-run setup requires the one-time setup token delivered through the server log or configured token file before creating the initial administrator. Google sign-in appears when configured.

![A distinct reviewer inspecting submitted SQL and choosing approval or rejection](review.png)

![Portcullis-branded email and password sign-in screen](login.png)

![First-administrator setup requiring a one-time setup token, email, display name and password](bootstrap.png)

The environment labels in these captures are illustrative; both targets use an isolated demo database.


## Capture and validation

These captures use synthetic accounts and an isolated PostgreSQL database. See the [capture guide](README.md) to refresh screenshots and GIFs, and the [validation record](../milestones/m1/validation.md) for browser file-saving evidence.
