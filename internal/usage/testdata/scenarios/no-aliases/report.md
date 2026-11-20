# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,073 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 84 distinct user agents.

- **Litware** made 20% of requests and received 33% of the data; 40% of its listing responses were compressed.
- 63 named organizations and products account for 50% of requests and 44% of the data. The rest cannot be tied to a name.
- Across all clients, 32% of listing responses were compressed, carrying 8.2% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 67% | 84% |
| List servers (v0) | 12% | 14% |
| Get one server version | 8.1% | 0.6% |
| List one server's versions | 4.9% | 0.3% |
| Other server calls | 4.4% | 0.3% |
| Docs and home page | 2.3% | 0.5% |
| Unknown paths | 0.5% | <0.1% |
| Health and version checks | 0.4% | <0.1% |
| Sign in | 0.2% | <0.1% |
| Publish | 0.2% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 36% | 57% |
| Paging through latest versions | 44% | 35% |
| First page only | 13% | 7.6% |
| Search by name | 2.9% | 0.5% |
| Incremental sync | 4.5% | 0.2% |

- **Page size:** limit=100 40%, limit=30 16%, limit=50 12%, other 31%.
- **Gateway cache:** 41% of listing requests were served from cache.
- **Responses:** success 95%, redirect or not modified 1.9%, not found (404) 1.8%, rate-limited (429) 1.0%, server error 0.4%, other client error 0.3%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent | 20% | 33% | 40% | 106 |
| Acme Widgets Inc | Network owner | 1.8% | 3.1% | 0% | 15 |
| proseware-sync | User agent | 2.4% | 2.9% | 0% | 14 |
| relecloud-host | User agent | 9.3% | 0.7% | – | 63 |
| adatum | User agent | 2.1% | 0.6% | 99.9% | 22 |
| Tailspin Toys | Network owner | 0.6% | 0.4% | 0% | 6 |
| Litware, Inc. | Network owner | 0.5% | 0.4% | 0% | 4 |
| Lakeshore Dispatch | Network owner | 0.5% | 0.3% | 0% | 3 |
| tool-48 | User agent | 0.4% | 0.3% | 0% | 2 |
| tool-39 | User agent | 0.4% | 0.3% | 0% | 3 |
| tool-30 | User agent | 0.3% | 0.2% | 0% | 1 |
| trey-agent | User agent | 0.8% | 0.2% | 0% | 6 |
| 51 other named |  | 11% | 1.4% | 41% | 186 |
| Unnamed clients on cloud hosting | Not identifiable | 40% | 52% | 32% | 170 |
| Unnamed clients on internet providers | Not identifiable | 9.6% | 4.0% | 11% | 87 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 19% | 31% | 42% | 103 | Litware, Inc. (78%) | Paging through all versions |
| Go-http-client | 10% | 23% | 0% | 58 | Google (84%) | Paging through all versions |
| python-requests | 13% | 22% | 0% | 78 | Amazon (AWS) (86%) | Paging through latest versions |
| (no user agent) | 2.9% | 3.6% | 0% | 33 | Amazon (AWS) (87%) | Paging through latest versions |
| node | 11% | 3.3% | 95% | 67 | Microsoft (Azure) (81%) | Paging through latest versions |
| proseware-sync | 2.4% | 2.9% | 0% | 14 | Microsoft (Azure) (100%) | Paging through latest versions |
| litware | 1.1% | 2.7% | 0% | 8 | Litware, Inc. (100%) | Paging through all versions |
| Python | 3.4% | 1.1% | 100% | 30 | Amazon (AWS) (100%) | Paging through latest versions |
| 76 other clients | 36% | 11% | 26% | 377 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 27% | 34% | 92 |
| Litware, Inc. | 16% | 26% | 80 |
| Google | 11% | 23% | 32 |
| Microsoft (Azure) | 13% | 5.7% | 32 |
| Acme Widgets Inc | 3.7% | 3.8% | 32 |
| Example Telecom Ltd | 5.9% | 1.9% | 32 |
| 39 other networks | 23% | 6.1% | 222 |

_Totals are from the gateway's request counters; shares are from 1,760 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._
