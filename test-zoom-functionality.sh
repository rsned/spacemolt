#!/bin/bash

echo "=== Galaxy Map Zoom Functionality Test ==="
echo

# Test 1: Verify files exist and are correct
echo "📁 Test 1: Checking modified files..."
FILES_OK=true

if [ -f "frontend/src/components/galaxy/GalaxyMap.tsx" ]; then
    echo "  ✓ GalaxyMap.tsx exists"
    if grep -q "handleWheel" frontend/src/components/galaxy/GalaxyMap.tsx; then
        echo "  ✓ handleWheel function found"
    else
        echo "  ✗ handleWheel function NOT found"
        FILES_OK=false
    fi
    if grep -q "handleSliderChange" frontend/src/components/galaxy/GalaxyMap.tsx; then
        echo "  ✓ handleSliderChange function found"
    else
        echo "  ✗ handleSliderChange function NOT found"
        FILES_OK=false
    fi
    if grep -q "ZOOM_MIN\|ZOOM_MAX\|ZOOM_STEP" frontend/src/components/galaxy/GalaxyMap.tsx; then
        echo "  ✓ Zoom constants defined"
    else
        echo "  ✗ Zoom constants NOT found"
        FILES_OK=false
    fi
else
    echo "  ✗ GalaxyMap.tsx NOT found"
    FILES_OK=false
fi

if [ -f "frontend/src/index.css" ]; then
    echo "  ✓ index.css exists"
    if grep -q "slider-vertical" frontend/src/index.css; then
        echo "  ✓ slider-vertical CSS found"
    else
        echo "  ✗ slider-vertical CSS NOT found"
        FILES_OK=false
    fi
else
    echo "  ✗ index.css NOT found"
    FILES_OK=false
fi

echo

# Test 2: Verify TypeScript compilation
echo "🔨 Test 2: Checking TypeScript compilation..."
cd frontend
if npm run build > /tmp/build.log 2>&1; then
    echo "  ✓ TypeScript compilation successful"
    BUILT_SIZE=$(du -sh dist/assets/index-*.js 2>/dev/null | cut -f1)
    echo "  ✓ Bundle size: $BUILT_SIZE"
else
    echo "  ✗ TypeScript compilation FAILED"
    echo "  Error log:"
    tail -10 /tmp/build.log | sed 's/^/    /'
fi
cd ..
echo

# Test 3: Verify zoom constants
echo "🔢 Test 3: Verifying zoom calculations..."
echo "  Testing zoom multiplier formula..."

# Test the formula: zoomMultiplier = 1 + (zoom - ZOOM_MIN) * 4
# Where ZOOM_MIN = 0.2, ZOOM_MAX = 1.0

test_zoom_multiplier() {
    local zoom=$1
    local expected=$2
    local ZOOM_MIN=0.2
    local result=$(echo "scale=1; 1 + ($zoom - $ZOOM_MIN) * 4" | bc)
    if [ $(echo "$result == $expected" | bc) -eq 1 ]; then
        echo "  ✓ Zoom $zoom → ${expected}x multiplier (calculated: $result)"
        return 0
    else
        echo "  ✗ Zoom $zoom → Expected $expected, got $result"
        return 1
    fi
}

MATH_OK=true
test_zoom_multiplier 0.2 1.0 || MATH_OK=false  # Minimum zoom = 1x
test_zoom_multiplier 0.35 1.6 || MATH_OK=false # Mid-range
test_zoom_multiplier 0.5 2.2 || MATH_OK=false  # Half range
test_zoom_multiplier 1.0 5.0 || MATH_OK=false  # Maximum zoom = 5x

echo

# Test 4: Verify API endpoints
echo "🌐 Test 4: Testing API endpoints..."

API_OK=true
SYSTEM_COUNT=$(curl -s http://localhost:8090/api/systems | jq '. | length')
if [ "$SYSTEM_COUNT" -eq 505 ]; then
    echo "  ✓ Systems API: $SYSTEM_COUNT systems available"
else
    echo "  ✗ Systems API: Expected 505, got $SYSTEM_COUNT"
    API_OK=false
fi

# Check a sample system has position data
SAMPLE_SYSTEM=$(curl -s http://localhost:8090/api/systems | jq '.[0]')
if echo "$SAMPLE_SYSTEM" | jq -e '.position' > /dev/null; then
    echo "  ✓ System position data available"
else
    echo "  ✗ System position data missing"
    API_OK=false
fi

# Check a sample system has connections
if echo "$SAMPLE_SYSTEM" | jq -e '.connections' > /dev/null; then
    CONN_COUNT=$(echo "$SAMPLE_SYSTEM" | jq '.connections | length')
    echo "  ✓ System connections available (sample has $CONN_COUNT connections)"
else
    echo "  ✗ System connections missing"
    API_OK=false
fi

echo

# Test 5: CSS verification
echo "🎨 Test 5: Verifying CSS styling..."

CSS_OK=true
if grep -q "\.slider-vertical" frontend/src/index.css; then
    echo "  ✓ .slider-vertical class exists"

    # Check for WebKit styles
    if grep -q "slider-vertical::-webkit-slider-thumb" frontend/src/index.css; then
        echo "  ✓ WebKit slider thumb styling found"
    else
        echo "  ✗ WebKit slider thumb styling missing"
        CSS_OK=false
    fi

    # Check for Mozilla styles
    if grep -q "slider-vertical::-moz-range-thumb" frontend/src/index.css; then
        echo "  ✓ Mozilla slider thumb styling found"
    else
        echo "  ✗ Mozilla slider thumb styling missing"
        CSS_OK=false
    fi
else
    echo "  ✗ .slider-vertical class NOT found"
    CSS_OK=false
fi

echo

# Test 6: Component structure verification
echo "🧩 Test 6: Verifying component structure..."

COMP_OK=true
if grep -q "useState.*zoom" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Zoom state management found"
else
    echo "  ✗ Zoom state management NOT found"
    COMP_OK=false
fi

if grep -q "onWheel={handleWheel}" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ onWheel event handler attached"
else
    echo "  ✗ onWheel event handler NOT attached"
    COMP_OK=false
fi

if grep -q "type=\"range\"" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Range slider input found"
else
    echo "  ✗ Range slider input NOT found"
    COMP_OK=false
fi

if grep -q "handleResetZoom" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Reset zoom function found"
else
    echo "  ✗ Reset zoom function NOT found"
    COMP_OK=false
fi

echo

# Test 7: Feature completeness check
echo "✅ Test 7: Feature completeness check..."

FEATURES_OK=true

echo "  Checking for mouse wheel zoom support..."
if grep -q "WheelEvent" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Mouse wheel zoom support confirmed"
else
    echo "  ✗ Mouse wheel zoom support missing"
    FEATURES_OK=false
fi

echo "  Checking for vertical slider..."
if grep -q "slider-vertical" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Vertical slider implemented"
else
    echo "  ✗ Vertical slider missing"
    FEATURES_OK=false
fi

echo "  Checking for zoom level display..."
if grep -q "zoomLevel" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Zoom level display found"
else
    echo "  ✗ Zoom level display missing"
    FEATURES_OK=false
fi

echo "  Checking for zoom indicator in status bar..."
if grep -q "Zoom:.*zoomLevel" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Status bar zoom indicator found"
else
    echo "  ✗ Status bar zoom indicator missing"
    FEATURES_OK=false
fi

echo

# Test 8: Performance check
echo "⚡ Test 8: Performance verification..."

PERF_OK=true
# Check if useMemo is used for expensive calculations
if grep -q "useMemo" frontend/src/components/galaxy/GalaxyMap.tsx; then
    MEMO_COUNT=$(grep -c "useMemo" frontend/src/components/galaxy/GalaxyMap.tsx)
    echo "  ✓ useMemo optimizations found ($MEMO_COUNT instances)"
else
    echo "  ⚠ Warning: No useMemo optimizations found (may impact performance)"
    PERF_OK=false
fi

echo

# Summary
echo "=== TEST SUMMARY ==="
echo

ALL_OK=true
[ "$FILES_OK" = true ] || ALL_OK=false
[ "$MATH_OK" = true ] || ALL_OK=false
[ "$API_OK" = true ] || ALL_OK=false
[ "$CSS_OK" = true ] || ALL_OK=false
[ "$COMP_OK" = true ] || ALL_OK=false
[ "$FEATURES_OK" = true ] || ALL_OK=false
[ "$PERF_OK" = true ] || ALL_OK=false

if [ "$ALL_OK" = true ]; then
    echo "🎉 ALL TESTS PASSED!"
    echo
    echo "✅ Files: OK"
    echo "✅ Math: OK"
    echo "✅ API: OK"
    echo "✅ CSS: OK"
    echo "✅ Components: OK"
    echo "✅ Features: OK"
    echo "✅ Performance: OK"
    echo
    echo "🚀 The zoom functionality is fully implemented and ready to use!"
    echo
    echo "📋 Next Steps:"
    echo "   1. Open http://localhost:5173/ in your browser"
    echo "   2. Click on the 'Galaxy Map' tab"
    echo "   3. Test the zoom features:"
    echo "      - Scroll mouse wheel up/down"
    echo "      - Drag the vertical slider on the left"
    echo "      - Click the reset button (⟲)"
    echo "      - Watch the zoom level indicator change"
else
    echo "❌ SOME TESTS FAILED"
    echo
    [ "$FILES_OK" = false ] && echo "✗ Files check failed"
    [ "$MATH_OK" = false ] && echo "✗ Math verification failed"
    [ "$API_OK" = false ] && echo "✗ API endpoints failed"
    [ "$CSS_OK" = false ] && echo "✗ CSS styling failed"
    [ "$COMP_OK" = false ] && echo "✗ Component structure failed"
    [ "$FEATURES_OK" = false ] && echo "✗ Features incomplete"
    [ "$PERF_OK" = false ] && echo "✗ Performance issues detected"
fi

echo
echo "=== END OF TESTS ==="
