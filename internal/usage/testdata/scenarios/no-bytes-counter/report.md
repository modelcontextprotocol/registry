# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,089 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 79 distinct user agents.

- **Litware** made 21% of requests and received 35% of the data; 38% of its listing responses were compressed.
- 61 named organizations and products account for 50% of requests and 43% of the data. The rest cannot be tied to a name.
- Across all clients, 34% of listing responses were compressed, carrying 8.6% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 67% | 84% |
| List servers (v0) | 12% | 14% |
| Get one server version | 8.4% | 0.6% |
| List one server's versions | 4.4% | 0.3% |
| Other server calls | 4.2% | 0.3% |
| Docs and home page | 2.5% | 0.4% |
| Unknown paths | 0.5% | <0.1% |
| Health and version checks | 0.4% | <0.1% |
| Publish | 0.2% | <0.1% |
| Sign in | 0.2% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 39% | 60% |
| Paging through latest versions | 44% | 34% |
| First page only | 10% | 5.3% |
| Search by name | 3.7% | 0.9% |
| Incremental sync | 3.4% | 0.2% |

- **Page size:** limit=100 41%, limit=30 17%, limit=50 13%, other 29%.
- **Gateway cache:** 43% of listing requests were served from cache.
- **Responses:** success 96%, not found (404) 1.6%, rate-limited (429) 1.0%, redirect or not modified 0.8%, server error 0.5%, other client error 0.3%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent and network owner | 21% | 35% | 38% | 107 |
| proseware-sync | User agent | 1.8% | 2.3% | 0% | 19 |
| relecloud-host | User agent | 9.8% | 0.7% | – | 64 |
| Tailspin Toys | Network owner | 0.8% | 0.6% | 0% | 6 |
| adatum | User agent | 2.2% | 0.5% | 100% | 14 |
| tool-48 | User agent | 0.4% | 0.3% | 0% | 2 |
| AS64511 | User agent and network owner | 0.6% | 0.3% | 0% | 13 |
| tool-42 | User agent | 0.4% | 0.3% | 0% | 2 |
| tool-12 | User agent | 0.4% | 0.3% | 0% | 3 |
| VanArsdel-Mirror | User agent | 2.6% | 0.2% | 82% | 17 |
| tool-57 | User agent | 0.3% | 0.2% | 0% | 1 |
| tool-29 | User agent | 0.6% | 0.2% | 0% | 3 |
| 49 other named |  | 9.0% | 2.0% | 9.6% | 150 |
| Unnamed clients on cloud hosting | Not identifiable | 42% | 54% | 34% | 159 |
| Unnamed clients on internet providers | Not identifiable | 8.1% | 2.7% | 21% | 91 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 20% | 34% | 39% | 103 | Litware (80%) | Paging through all versions |
| Go-http-client | 11% | 24% | 0% | 65 | Google (88%) | Paging through all versions |
| python-requests | 12% | 19% | 0% | 63 | Amazon (AWS) (98%) | Paging through latest versions |
| (no user agent) | 4.1% | 5.5% | 0% | 31 | Amazon (AWS) (67%) | Paging through latest versions |
| node | 12% | 3.3% | 98% | 68 | Microsoft (Azure) (83%) | Paging through latest versions |
| proseware-sync | 1.8% | 2.3% | 0% | 19 | Microsoft (Azure) (100%) | Paging through latest versions |
| litware | 0.6% | 1.5% | 0% | 7 | Litware (100%) | Paging through all versions |
| Python | 4.0% | 1.3% | 100% | 34 | Amazon (AWS) (100%) | Paging through latest versions |
| 71 other clients | 34% | 9.8% | 31% | 321 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 27% | 33% | 87 |
| Litware | 17% | 28% | 84 |
| Google | 13% | 25% | 32 |
| Microsoft (Azure) | 13% | 5.3% | 32 |
| Acme Widgets Inc | 2.6% | 1.2% | 26 |
| Example Telecom Ltd | 5.1% | 1.0% | 32 |
| 38 other networks | 23% | 6.9% | 214 |

_Totals are from the gateway's request counters; shares are from 1,733 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Gateway counters were unavailable, so totals are estimated from logs alone.
