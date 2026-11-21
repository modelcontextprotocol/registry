# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,073 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 61 distinct user agents.

- **Litware** made 15% of requests and received 26% of the data; 39% of its listing responses were compressed.
- 42 named organizations and products account for 45% of requests and 40% of the data. The rest cannot be tied to a name.
- Across all clients, 31% of listing responses were compressed, carrying 8.6% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 68% | 84% |
| List servers (v0) | 12% | 14% |
| Get one server version | 8.0% | 0.6% |
| Other server calls | 4.1% | 0.3% |
| List one server's versions | 4.1% | 0.3% |
| Docs and home page | 2.5% | 0.5% |
| Unknown paths | 0.5% | <0.1% |
| Sign in | 0.2% | <0.1% |
| Health and version checks | 0.2% | <0.1% |
| Publish | 0.1% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 29% | 48% |
| Paging through latest versions | 53% | 44% |
| First page only | 12% | 8.0% |
| Search by name | 2.9% | 0.4% |
| Incremental sync | 3.4% | <0.1% |

- **Page size:** limit=100 40%, limit=30 15%, limit=10 15%, other 30%.
- **Gateway cache:** 36% of listing requests were served from cache.
- **Responses:** success 95%, redirect or not modified 2.0%, not found (404) 1.9%, rate-limited (429) 0.9%, server error 0.5%, other client error 0.2%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent | 15% | 26% | 39% | 44 |
| Acme Widgets Inc | Network owner | 2.9% | 5.3% | 0% | 7 |
| proseware-sync | User agent | 3.4% | 4.5% | 0% | 8 |
| adatum | User agent | 3.3% | 1.1% | 100% | 12 |
| Tailspin Toys | Network owner | 1.2% | 1.0% | 0% | 5 |
| relecloud-host | User agent | 8.5% | 0.6% | – | 57 |
| tool-39 | User agent | 0.3% | 0.3% | 0% | 1 |
| ExampleBot | User agent | 0.6% | 0.2% | – | 9 |
| trey-agent | User agent | 0.7% | 0.1% | 0% | 3 |
| tool-59 | User agent | 0.4% | 0.1% | 0% | 1 |
| tool-51 | User agent | 0.1% | 0.1% | 0% | 2 |
| VanArsdel-Mirror | User agent | 2.7% | <0.1% | 100% | 8 |
| 30 other named |  | 5.6% | 0.5% | 0% | 64 |
| Unnamed clients on cloud hosting | Not identifiable | 44% | 54% | 33% | 94 |
| Unnamed clients on internet providers | Not identifiable | 11% | 6.2% | 0% | 59 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| python-requests | 15% | 26% | 0% | 33 | Amazon (AWS) (81%) | Paging through latest versions |
| Litware | 15% | 25% | 40% | 43 | Litware (74%) | Paging through all versions |
| Go-http-client | 10% | 22% | 0% | 31 | Google (78%) | Paging through all versions |
| node | 16% | 6.2% | 85% | 43 | Microsoft (Azure) (77%) | Paging through latest versions |
| proseware-sync | 3.4% | 4.5% | 0% | 8 | Microsoft (Azure) (100%) | Paging through latest versions |
| (no user agent) | 3.8% | 3.5% | 0% | 11 | Amazon (AWS) (89%) | Paging through latest versions |
| (identifier withheld) | 2.4% | 2.2% | 0% | 5 | Amazon (AWS) (100%) | First page only |
| okhttp | 1.4% | 1.2% | 0% | 8 | Tailspin Toys (87%) | First page only |
| 53 other clients | 33% | 8.9% | 46% | 211 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 29% | 36% | 61 |
| Google | 9.8% | 21% | 24 |
| Litware | 11% | 20% | 37 |
| Microsoft (Azure) | 17% | 8.4% | 24 |
| Acme Widgets Inc | 4.7% | 5.8% | 17 |
| Small Telecom 15 | 1.4% | 1.5% | 2 |
| 32 other networks | 27% | 8.0% | 164 |

_Totals are from the gateway's request counters; shares are from 582 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._
