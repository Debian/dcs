#!/bin/zsh

# querylog-benchpos.txt contains only queries for which query_pos is true, to
# benchmark positional index querying:
# % dcs replay -log querylog-full.txt -pos > pos.json
# % jq -r '. | select(.query_pos) | .query' < pos.json > querylog-benchpos.txt

echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "old index (index only):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt -old -skip_file > old.json  2>/dev/null

echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "old index (index + i/o):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt -old -skip_matching > old.json 2>/dev/null

echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "old index (index + i/o + matching):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt -old > old.json 2>/dev/null


echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "new index (non-positional) (index only):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt -skip_file > new.json 2>/dev/null

echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "new index (non-positional) (index + i/o):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt -skip_matching > new.json 2>/dev/null

echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "new index (non-positional) (index + i/o + matching):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt > new.json 2>/dev/null

echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "new index (positional) (index only):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt -pos -skip_file > pos.json 2>/dev/null

echo 3 | sudo tee /proc/sys/vm/drop_caches
echo "new index (positional) (index + (i/o + matching)):"
time dcs -listen=localhost:6066 replay -log=querylog-benchpos.txt -pos > pos.json 2>/dev/null
