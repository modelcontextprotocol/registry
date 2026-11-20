# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,089 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 79 distinct user agents.

- **Litware** made 22% of requests and received 36% of the data; 38% of its listing responses were compressed.
- 61 named organizations and products account for 50% of requests and 44% of the data. The rest cannot be tied to a name.
- Across all clients, 35% of listing responses were compressed, carrying 9.0% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 68% | 84% |
| List servers (v0) | 12% | 14% |
| Get one server version | 8.2% | 0.6% |
| List one server's versions | 4.3% | 0.3% |
| Other server calls | 4.1% | 0.3% |
| Docs and home page | 2.4% | 0.4% |
| Unknown paths | 0.5% | <0.1% |
| Health and version checks | 0.4% | <0.1% |
| Publish | 0.2% | <0.1% |
| Sign in | 0.2% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 39% | 60% |
| Paging through latest versions | 44% | 33% |
| First page only | 11% | 5.4% |
| Search by name | 3.6% | 0.9% |
| Incremental sync | 3.2% | 0.2% |

- **Page size:** limit=100 41%, limit=30 16%, limit=50 13%, other 29%.
- **Gateway cache:** 42% of listing requests were served from cache.
- **Responses:** success 96%, not found (404) 1.6%, rate-limited (429) 0.9%, redirect or not modified 0.8%, server error 0.5%, other client error 0.3%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent and network owner | 22% | 36% | 38% | 107 |
| proseware-sync | User agent | 1.8% | 2.4% | 0% | 19 |
| relecloud-host | User agent | 9.5% | 0.7% | – | 64 |
| Tailspin Toys | Network owner | 0.7% | 0.5% | 0% | 6 |
| adatum | User agent | 2.0% | 0.5% | 99.9% | 14 |
| tool-48 | User agent | 0.4% | 0.3% | 0% | 2 |
| AS64511 | User agent and network owner | 0.7% | 0.3% | 0% | 13 |
| tool-12 | User agent | 0.4% | 0.3% | 0% | 3 |
| tool-42 | User agent | 0.4% | 0.3% | 0% | 2 |
| tool-29 | User agent | 0.6% | 0.2% | 0% | 3 |
| VanArsdel-Mirror | User agent | 2.5% | 0.2% | 86% | 17 |
| Acme Widgets Inc | Network owner | 0.2% | 0.2% | 0% | 3 |
| 49 other named |  | 8.8% | 1.9% | 11% | 149 |
| Unnamed clients on cloud hosting | Not identifiable | 42% | 53% | 36% | 159 |
| Unnamed clients on internet providers | Not identifiable | 8.2% | 2.8% | 20% | 91 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 21% | 34% | 40% | 103 | Litware (82%) | Paging through all versions |
| Go-http-client | 11% | 23% | 0% | 65 | Google (88%) | Paging through all versions |
| python-requests | 12% | 18% | 0% | 63 | Amazon (AWS) (98%) | Paging through latest versions |
| (no user agent) | 4.0% | 5.5% | 0% | 31 | Amazon (AWS) (68%) | Paging through latest versions |
| node | 12% | 3.4% | 98% | 68 | Microsoft (Azure) (84%) | Paging through latest versions |
| proseware-sync | 1.8% | 2.4% | 0% | 19 | Microsoft (Azure) (100%) | Paging through latest versions |
| litware | 0.7% | 1.7% | 0% | 7 | Litware (100%) | Paging through all versions |
| Python | 4.5% | 1.5% | 100% | 34 | Amazon (AWS) (100%) | Paging through latest versions |
| 71 other clients | 33% | 9.6% | 30% | 321 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 26% | 32% | 87 |
| Litware | 18% | 29% | 84 |
| Google | 13% | 24% | 32 |
| Microsoft (Azure) | 13% | 5.5% | 32 |
| Acme Widgets Inc | 2.5% | 1.1% | 26 |
| Example Telecom Ltd | 5.1% | 1.0% | 32 |
| 38 other networks | 22% | 6.7% | 214 |

_Totals are from the gateway's request counters; shares are from 1,733 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Hourly gateway counters were unavailable, so one scaling factor was used for the whole period.
