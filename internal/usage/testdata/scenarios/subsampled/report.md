# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,073 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 84 distinct user agents.

- **Litware** made 15% of requests and received 33% of the data; 37% of its listing responses were compressed.
- 62 named organizations and products account for 55% of requests and 45% of the data. The rest cannot be tied to a name.
- Across all clients, 31% of listing responses were compressed, carrying 8.0% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 50% | 82% |
| Get one server version | 16% | 1.5% |
| List one server's versions | 9.6% | 0.9% |
| List servers (v0) | 8.6% | 13% |
| Other server calls | 8.6% | 0.8% |
| Docs and home page | 4.5% | 1.3% |
| Unknown paths | 1.0% | <0.1% |
| Health and version checks | 0.8% | <0.1% |
| Sign in | 0.4% | <0.1% |
| Publish | 0.4% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 36% | 56% |
| Paging through latest versions | 44% | 35% |
| First page only | 13% | 7.5% |
| Search by name | 3.0% | 0.5% |
| Incremental sync | 4.5% | 0.2% |

- **Page size:** limit=100 40%, limit=30 16%, limit=50 12%, other 31%.
- **Gateway cache:** 40% of listing requests were served from cache.
- **Responses:** success 92%, not found (404) 3.5%, rate-limited (429) 2.0%, redirect or not modified 1.6%, server error 0.8%, other client error 0.5%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent and network owner | 15% | 33% | 37% | 108 |
| proseware-sync | User agent | 1.9% | 3.0% | 0% | 14 |
| Acme Widgets Inc | Network owner | 1.4% | 2.9% | 0% | 15 |
| relecloud-host | User agent | 18% | 1.7% | – | 63 |
| adatum | User agent | 1.7% | 0.7% | 100% | 22 |
| Tailspin Toys | Network owner | 0.4% | 0.4% | 0% | 6 |
| ExampleBot | User agent | 1.4% | 0.4% | 99.9% | 20 |
| tool-39 | User agent | 0.3% | 0.3% | 0% | 3 |
| Lakeshore Dispatch | Network owner | 0.3% | 0.3% | 0% | 3 |
| tool-48 | User agent | 0.3% | 0.3% | 0% | 2 |
| VanArsdel-Mirror | User agent | 2.6% | 0.2% | 81% | 18 |
| trey-agent | User agent | 0.5% | 0.2% | 0% | 6 |
| 50 other named |  | 11% | 1.7% | 0% | 163 |
| Unnamed clients on cloud hosting | Not identifiable | 32% | 50% | 32% | 168 |
| Unnamed clients on internet providers | Not identifiable | 13% | 4.8% | 10% | 87 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 14% | 31% | 40% | 103 | Litware (80%) | Paging through all versions |
| Go-http-client | 9.0% | 22% | 0% | 58 | Google (70%) | Paging through all versions |
| python-requests | 9.6% | 21% | 0% | 78 | Amazon (AWS) (86%) | Paging through latest versions |
| (no user agent) | 2.4% | 4.0% | 0% | 33 | Amazon (AWS) (89%) | Paging through latest versions |
| node | 10% | 3.3% | 95% | 67 | Microsoft (Azure) (64%) | Paging through latest versions |
| proseware-sync | 1.9% | 3.0% | 0% | 14 | Microsoft (Azure) (100%) | Paging through latest versions |
| litware | 0.7% | 2.4% | 0% | 8 | Litware (100%) | Paging through all versions |
| relecloud-host | 18% | 1.7% | – | 63 | Northwind Fiber Co-op (AS64502) (72%) | Get one server version |
| 76 other clients | 34% | 13% | 39% | 327 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 21% | 32% | 90 |
| Litware | 12% | 26% | 82 |
| Google | 10% | 22% | 32 |
| Microsoft (Azure) | 11% | 5.9% | 32 |
| Acme Widgets Inc | 3.4% | 3.6% | 32 |
| Example Telecom Ltd | 8.7% | 2.6% | 32 |
| 39 other networks | 34% | 7.5% | 222 |

_Totals are from the gateway's request counters; shares are from 1,760 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Log-based request count differs from the gateway counters by +36%; treat the shares as approximate.
