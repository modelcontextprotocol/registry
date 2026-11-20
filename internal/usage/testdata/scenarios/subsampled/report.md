# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,089 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 79 distinct user agents.

- **Litware** made 16% of requests and received 35% of the data; 37% of its listing responses were compressed.
- 61 named organizations and products account for 56% of requests and 45% of the data. The rest cannot be tied to a name.
- Across all clients, 34% of listing responses were compressed, carrying 8.5% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 50% | 82% |
| Get one server version | 17% | 1.5% |
| List servers (v0) | 8.8% | 14% |
| List one server's versions | 8.7% | 0.8% |
| Other server calls | 8.4% | 0.8% |
| Docs and home page | 4.9% | 1.1% |
| Unknown paths | 1.1% | <0.1% |
| Health and version checks | 0.8% | <0.1% |
| Publish | 0.5% | <0.1% |
| Sign in | 0.4% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 38% | 59% |
| Paging through latest versions | 44% | 34% |
| First page only | 11% | 5.4% |
| Search by name | 3.7% | 0.9% |
| Incremental sync | 3.5% | 0.3% |

- **Page size:** limit=100 41%, limit=30 16%, limit=50 13%, other 29%.
- **Gateway cache:** 43% of listing requests were served from cache.
- **Responses:** success 93%, not found (404) 3.2%, rate-limited (429) 1.9%, server error 1.0%, redirect or not modified 0.7%, other client error 0.5%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent and network owner | 16% | 35% | 37% | 107 |
| proseware-sync | User agent | 1.7% | 2.9% | 0% | 19 |
| relecloud-host | User agent | 19% | 1.8% | – | 64 |
| Tailspin Toys | Network owner | 0.6% | 0.6% | 0% | 6 |
| adatum | User agent | 1.6% | 0.5% | 100% | 14 |
| ExampleBot | User agent | 1.6% | 0.4% | 100% | 24 |
| Acme Widgets Inc | Network owner | 0.2% | 0.3% | 0% | 3 |
| VanArsdel-Mirror | User agent | 2.1% | 0.3% | 80% | 17 |
| AS64511 | User agent and network owner | 0.8% | 0.3% | 0% | 13 |
| tool-48 | User agent | 0.2% | 0.3% | 0% | 2 |
| tool-42 | User agent | 0.2% | 0.2% | 0% | 2 |
| tool-12 | User agent | 0.2% | 0.2% | 0% | 3 |
| 49 other named |  | 12% | 2.4% | 1.2% | 144 |
| Unnamed clients on cloud hosting | Not identifiable | 32% | 51% | 35% | 159 |
| Unnamed clients on internet providers | Not identifiable | 13% | 3.7% | 18% | 91 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 15% | 33% | 38% | 103 | Litware (82%) | Paging through all versions |
| Go-http-client | 9.4% | 22% | 0% | 65 | Google (73%) | Paging through all versions |
| python-requests | 8.8% | 18% | 0% | 63 | Amazon (AWS) (97%) | Paging through latest versions |
| (no user agent) | 3.0% | 5.5% | 0% | 31 | Amazon (AWS) (70%) | Paging through latest versions |
| node | 11% | 3.4% | 98% | 68 | Microsoft (Azure) (67%) | Paging through latest versions |
| proseware-sync | 1.7% | 2.9% | 0% | 19 | Microsoft (Azure) (100%) | Paging through latest versions |
| relecloud-host | 19% | 1.8% | – | 64 | Northwind Fiber Co-op (AS64502) (72%) | Get one server version |
| litware | 0.5% | 1.5% | 0% | 7 | Litware (100%) | Paging through all versions |
| 71 other clients | 31% | 11% | 45% | 282 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 20% | 31% | 87 |
| Litware | 13% | 28% | 84 |
| Google | 11% | 23% | 32 |
| Microsoft (Azure) | 11% | 5.9% | 32 |
| Example Telecom Ltd | 8.2% | 1.7% | 32 |
| Northwind Fiber Co-op (AS64502) | 14% | 1.3% | 32 |
| 38 other networks | 23% | 8.1% | 208 |

_Totals are from the gateway's request counters; shares are from 1,733 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Log-based request count differs from the gateway counters by +35%; treat the shares as approximate.
