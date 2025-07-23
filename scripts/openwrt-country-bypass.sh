# download
cd /tmp
wget https://github.com/kkrow/maxminddb-to-nft/releases/download/latest/geoip_by_country.tar.gz -O geoip_by_country.tar.gz
tar xzf geoip_by_country.tar.gz by_country/US/US_ipv4.nft
mv by_country/US/US_ipv4.nft .
rm -rf by_country/
rm geoip_by_country.tar.gz

# del old rules
ip rule del fwmark 0x1 table rt_us
ip route flush table rt_us
nft delete table inet geoip

# add new rules
nft add table inet geoip
nft -f US_ipv4.nft
nft add chain inet geoip prerouting { type filter hook prerouting priority mangle \; }
ip route add default dev pppoe-pppoe table rt_us
ip rule add fwmark 0x1 table rt_us
