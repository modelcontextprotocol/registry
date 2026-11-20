# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,180 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 84 distinct user agents.

- **Litware** made 21% of requests and received 35% of the data; 36% of its listing responses were compressed.
- 62 named organizations and products account for 50% of requests and 46% of the data. The rest cannot be tied to a name.
- Across all clients, 32% of listing responses were compressed, carrying 8.3% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 68% | 84% |
| List servers (v0) | 12% | 15% |
| Get one server version | 7.8% | 0.5% |
| List one server's versions | 4.7% | 0.3% |
| Other server calls | 4.2% | 0.3% |
| Docs and home page | 2.2% | 0.5% |
| Unknown paths | 0.5% | <0.1% |
| Health and version checks | 0.4% | <0.1% |
| Sign in | 0.2% | <0.1% |
| Publish | 0.2% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 36% | 57% |
| Paging through latest versions | 44% | 35% |
| First page only | 13% | 7.5% |
| Search by name | 2.7% | 0.5% |
| Incremental sync | 4.0% | 0.2% |

- **Page size:** limit=100 40%, limit=30 16%, limit=50 12%, other 31%.
- **Gateway cache:** 39% of listing requests were served from cache.
- **Responses:** success 95%, redirect or not modified 1.8%, not found (404) 1.7%, rate-limited (429) 0.9%, server error 0.4%, other client error 0.3%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent and network owner | 21% | 35% | 36% | 108 |
| proseware-sync | User agent | 2.6% | 3.2% | 0% | 14 |
| Acme Widgets Inc | Network owner | 1.9% | 3.2% | 0% | 15 |
| relecloud-host | User agent | 8.9% | 0.6% | – | 63 |
| adatum | User agent | 2.0% | 0.6% | 100% | 22 |
| Tailspin Toys | Network owner | 0.6% | 0.5% | 0% | 6 |
| tool-39 | User agent | 0.4% | 0.3% | 0% | 3 |
| tool-48 | User agent | 0.4% | 0.3% | 0% | 2 |
| Lakeshore Dispatch | Network owner | 0.4% | 0.3% | 0% | 3 |
| trey-agent | User agent | 0.8% | 0.2% | 0% | 6 |
| tool-30 | User agent | 0.2% | 0.2% | 0% | 1 |
| ExampleBot | User agent | 0.8% | 0.2% | 100% | 20 |
| 50 other named |  | 9.6% | 1.2% | 36% | 180 |
| Unnamed clients on cloud hosting | Not identifiable | 41% | 50% | 34% | 168 |
| Unnamed clients on internet providers | Not identifiable | 9.6% | 3.9% | 12% | 87 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 19% | 31% | 40% | 103 | Litware (80%) | Paging through all versions |
| Go-http-client | 10% | 22% | 0% | 58 | Google (85%) | Paging through all versions |
| python-requests | 13% | 21% | 0% | 78 | Amazon (AWS) (85%) | Paging through latest versions |
| (no user agent) | 3.1% | 3.8% | 0% | 33 | Amazon (AWS) (87%) | Paging through latest versions |
| litware | 1.4% | 3.4% | 0% | 8 | Litware (100%) | Paging through all versions |
| node | 12% | 3.4% | 96% | 67 | Microsoft (Azure) (83%) | Paging through latest versions |
| proseware-sync | 2.6% | 3.2% | 0% | 14 | Microsoft (Azure) (100%) | Paging through latest versions |
| Python | 3.8% | 1.2% | 100% | 30 | Amazon (AWS) (100%) | Paging through latest versions |
| 76 other clients | 35% | 11% | 25% | 377 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 26% | 32% | 90 |
| Litware | 17% | 28% | 82 |
| Google | 11% | 22% | 32 |
| Microsoft (Azure) | 14% | 6.3% | 32 |
| Acme Widgets Inc | 3.6% | 3.8% | 32 |
| Example Telecom Ltd | 5.9% | 1.9% | 32 |
| 39 other networks | 22% | 6.0% | 222 |

_Totals are from the gateway's request counters; shares are from 1,760 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Gateway counters were unavailable, so totals are estimated from logs alone.
