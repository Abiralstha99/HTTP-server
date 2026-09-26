#!/bin/bash

echo "Sending 5 concurrent requests..."

for i in {1..5}
do
    echo "Starting request $i"

    curl http://127.0.0.1:8000/index.html &
done

wait

echo "All requests finished!"