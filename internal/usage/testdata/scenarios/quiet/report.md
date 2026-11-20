# Registry usage, Tue Oct 6, 2026 (UTC day)

**350 requests, 0.0 GiB sent** (about $0 at roughly $0.10 per GiB), from 61 distinct user agents.

- **Litware** made 24% of requests and received 59% of the data; 17% of its listing responses were compressed.
- 44 named organizations and products account for 67% of requests and 76% of the data. The rest cannot be tied to a name.
- Across all clients, 19% of listing responses were compressed, carrying 6.6% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 67% | 93% |
| Get one server version | 12% | 0.9% |
| List servers (v0) | 7.0% | 4.3% |
| List one server's versions | 5.8% | 0.5% |
| Other server calls | 4.7% | 0.4% |
| Docs and home page | 2.0% | 0.4% |
| Unknown paths | 0.9% | <0.1% |
| Publish | 0.3% | <0.1% |
| Health and version checks | 0.3% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 33% | 62% |
| Paging through latest versions | 25% | 19% |
| First page only | 20% | 12% |
| Incremental sync | 7.9% | 3.6% |
| Search by name | 14% | 2.9% |

- **Page size:** limit=100 43%, limit=50 15%, limit=20 14%, other 28%.
- **Gateway cache:** 33% of listing requests were served from cache.
- **Responses:** success 97%, not found (404) 1.2%, rate-limited (429) 1.2%, other client error 0.6%, redirect or not modified 0.3%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent | 24% | 59% | 17% | 7 |
| proseware-sync | User agent | 1.5% | 3.6% | 0% | 1 |
| VanArsdel-Mirror | User agent | 5.8% | 3.5% | 0% | 1 |
| tool-45 | User agent | 2.6% | 2.3% | 0% | 1 |
| tool-36 | User agent | 2.7% | 2.2% | 0% | 1 |
| adatum | User agent | 2.6% | 1.3% | 100% | 1 |
| tool-44 | User agent | 3.8% | 1.2% | 0% | 1 |
| tool-50 | User agent | 2.5% | 1.0% | 0% | 1 |
| relecloud-host | User agent | 7.3% | 0.6% | – | 19 |
| tool-32 | User agent | 2.6% | 0.4% | 0% | 1 |
| tool-3 | User agent | 0.3% | 0.3% | 0% | 1 |
| Spacebot | User agent | 0.3% | 0.2% | – | 1 |
| 32 other named |  | 11% | 0.9% | 0% | 35 |
| Unnamed clients on cloud hosting | Not identifiable | 23% | 21% | 37% | 28 |
| Unnamed clients on internet providers | Not identifiable | 9.6% | 3.1% | 0% | 16 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 24% | 59% | 17% | 7 | Litware (67%) | Paging through all versions |
| python-requests | 6.3% | 9.8% | 0% | 7 | Amazon (AWS) (72%) | Paging through latest versions |
| proseware-sync | 1.5% | 3.6% | 0% | 1 | Microsoft (Azure) (100%) | Paging through latest versions |
| VanArsdel-Mirror | 5.8% | 3.5% | 0% | 1 | Fabrikam Hosting (100%) | Incremental sync |
| (identifier withheld) | 3.0% | 3.0% | 0% | 2 | Amazon (AWS) (100%) | First page only |
| undici | 3.8% | 2.7% | 0% | 1 | Unknown (100%) | First page only |
| Python | 6.0% | 2.6% | 100% | 2 | Amazon (AWS) (100%) | Paging through latest versions |
| tool-45 | 2.6% | 2.3% | 0% | 1 | Amazon (AWS) (100%) | First page only |
| 53 other clients | 47% | 14% | 19% | 89 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 32% | 40% | 22 |
| Litware | 16% | 37% | 5 |
| Microsoft (Azure) | 7.8% | 6.6% | 10 |
| Fabrikam Hosting | 5.8% | 3.5% | 1 |
| Unknown | 4.4% | 2.7% | 3 |
| Google | 6.1% | 2.1% | 11 |
| 19 other networks | 28% | 7.5% | 56 |

_Totals are from the gateway's request counters; shares are from 127 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._
