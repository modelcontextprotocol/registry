# Registry usage, Tue Oct 6, 2026 (UTC day)

**1,537 requests, 114978094596.7 GiB sent** (about $11,497,809,460 at roughly $0.10 per GiB), from 27 distinct user agents.

- **Litware** made 1.3% of requests and received <0.1% of the data; 0% of its listing responses were compressed.
- 18 named organizations and products account for 12% of requests and <0.1% of the data. The rest cannot be tied to a name.
- Across all clients, 0.7% of listing responses were compressed, carrying <0.1% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 88% | 99.9% |
| List servers (v0) | 9.1% | <0.1% |
| Unknown paths | 1.4% | <0.1% |
| List one server's versions | 0.8% | <0.1% |
| Other server calls | 0.3% | <0.1% |
| Get one server version | 0.3% | <0.1% |
| Docs and home page | 0.2% | <0.1% |
| Publish | 0.1% | <0.1% |

Under 0.1% each: sign in (about 1 requests), health and version checks (about 1 requests).

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| First page only | 94% | 99.9% |
| Paging through all versions | 3.4% | <0.1% |
| Search by name | 2.0% | <0.1% |
| Paging through latest versions | 0.7% | <0.1% |

- **Page size:** default (30) 80%, limit=100 8.7%, limit=?ZZ 0.7%, other 10%.
- **Gateway cache:** 2.0% of listing requests were served from cache.
- **Responses:** success 98%, redirect or not modified 1.3%, other 0.5%, server error <0.1%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | Network owner | 1.3% | <0.1% | 0% | 2 |
| AS64511 | Network owner | 0.7% | <0.1% | 0% | 1 |
| TabBot | User agent | 0.7% | <0.1% | 0% | 1 |
| EmBot | User agent | 0.7% | <0.1% | 0% | 1 |
| a | User agent | 0.7% | <0.1% | 0% | 1 |
| mozilla | User agent | 0.7% | <0.1% | 0% | 1 |
| ?? | User agent | 0.7% | <0.1% | 0% | 1 |
| na?ve?client | User agent | 0.7% | <0.1% | 0% | 1 |
| xxxxxxxxxxxxxxxxxxxxxxxxxxxx | User agent | 0.7% | <0.1% | 0% | 1 |
| ???????????????????????????? | User agent | 0.7% | <0.1% | 0% | 1 |
| a?b?script? | User agent | 0.7% | <0.1% | 0% | 1 |
| ?6 | User agent | 0.7% | <0.1% | 0% | 1 |
| 6 other named |  | 3.9% | <0.1% | 0% | 1 |
| Unnamed clients on cloud hosting | Not identifiable | 77% | 99.9% | 0.9% | 40 |
| Unnamed clients on internet providers | Not identifiable | 11% | <0.1% | 0% | 16 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| curl | 78% | 99.9% | 0.9% | 59 | Amazon (AWS) (83%) | First page only |
| (no user agent) | 3.3% | <0.1% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| Web browser | 2.6% | <0.1% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| (not recorded) | 1.3% | <0.1% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| TabBot | 0.7% | <0.1% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| EmBot | 0.7% | <0.1% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| a | 0.7% | <0.1% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| mozilla | 0.7% | <0.1% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| 19 other clients | 12% | <0.1% | 0% | 1 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 87% | 99.9% | 40 |
| Unknown | 11% | <0.1% | 16 |
| Litware | 1.3% | <0.1% | 2 |
| 1 other networks | 0.7% | <0.1% | 1 |

_Totals are from the gateway's request counters; shares are from 214 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Gateway counters were unavailable, so totals are estimated from logs alone.
- One request ID repeats across many requests, which can skew the log sample.
- Some records predate user-agent logging (Nov 12 09:10 UTC) and show as "(not recorded)".
