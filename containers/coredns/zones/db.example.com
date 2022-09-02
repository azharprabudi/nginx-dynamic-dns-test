$TTL    604800
@    IN    SOA    ns1.example.com. admin.example.com. (
			 3   ; Serial
             604800        ; Refresh
              86400        ; Retry
            2419200        ; Expire
             604800 )    ; Negative Cache TTL
;

; name servers - NS records
@    IN    NS    ns1

; name servers - A records
ns1.example.com.                                        IN      A       172.16.240.10

wsserver.example.com.                                   IN      A       172.16.238.10

wsserver.example.com.                                   IN      A       172.16.238.11

