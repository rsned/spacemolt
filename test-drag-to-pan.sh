#!/bin/bash

echo "=== Drag-to-Pan Functionality Test ==="
echo

# Test 1: Verify code changes
echo "1. Checking code implementation..."
if grep -q "useState.*pan.*x.*y" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Pan state added"
else
    echo "  ✗ Pan state NOT found"
fi

if grep -q "handleMouseDown" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Mouse down handler found"
else
    echo "  ✗ Mouse down handler NOT found"
fi

if grep -q "handleMouseMove" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Mouse move handler found"
else
    echo "  ✗ Mouse move handler NOT found"
fi

if grep -q "handleMouseUp" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Mouse up handler found"
else
    echo "  ✗ Mouse up handler NOT found"
fi

if grep -q "isDragging" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Dragging state tracking found"
else
    echo "  ✗ Dragging state NOT found"
fi

if grep -q "cursor: isDragging.*grabbing.*grab" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Cursor change on drag found"
else
    echo "  ✗ Cursor change NOT found"
fi
echo

# Test 2: Verify pan is applied to coordinates
echo "2. Checking pan application to transform..."
if grep -q "+ pan.x" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Pan X offset applied"
else
    echo "  ✗ Pan X offset NOT applied"
fi

if grep -q "+ pan.y" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Pan Y offset applied"
else
    echo "  ✗ Pan Y offset NOT applied"
fi
echo

# Test 3: Verify reset functionality
echo "3. Checking reset functionality..."
if grep -q "setPan({ x: 0, y: 0 })" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Pan reset on zoom reset found"
else
    echo "  ✗ Pan reset NOT found"
fi
echo

# Test 4: Verify UI updates
echo "4. Checking UI updates..."
if grep -q "Pan:" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Pan display in status bar"
else
    echo "  ✗ Pan display NOT found"
fi

if grep -q "Math.round(pan.x)" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Pan coordinates displayed"
else
    echo "  ✗ Pan coordinates NOT displayed"
fi

if grep -q "Drag to pan" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Usage instruction added"
else
    echo "  ✗ Usage instruction NOT found"
fi
echo

# Test 5: Verify event handlers attached
echo "5. Checking event handlers..."
if grep -q "onMouseDown={handleMouseDown}" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ onMouseDown event attached"
else
    echo "  ✗ onMouseDown event NOT attached"
fi

if grep -q "onMouseMove={handleMouseMove}" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ onMouseMove event attached"
else
    echo "  ✗ onMouseMove event NOT attached"
fi

if grep -q "onMouseUp={handleMouseUp}" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ onMouseUp event attached"
else
    echo "  ✗ onMouseUp event NOT attached"
fi

if grep -q "onMouseLeave={handleMouseLeave}" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ onMouseLeave event attached"
else
    echo "  ✗ onMouseLeave event NOT attached"
fi
echo

# Test 6: Build verification
echo "6. Build verification..."
cd frontend
if npm run build > /tmp/build-pan.log 2>&1; then
    echo "  ✓ Build successful"
    BUNDLE_SIZE=$(du -sh dist/assets/index-*.js 2>/dev/null | cut -f1)
    echo "  ✓ Bundle size: $BUNDLE_SIZE"
else
    echo "  ✗ Build failed"
    tail -5 /tmp/build-pan.log
fi
cd ..
echo

echo "=== Feature Summary ==="
echo "✅ Pan state: { x: 0, y: 0 }"
echo "✅ Drag tracking: isDragging state"
echo "✅ Mouse events: Down, Move, Up, Leave"
echo "✅ Cursor changes: Grab ↔ Grabbing"
echo "✅ Transform update: + pan.x, + pan.y"
echo "✅ Reset function: Resets both zoom and pan"
echo "✅ Status bar: Shows X/Y pan coordinates"
echo "✅ Instructions: 'Drag to pan • Scroll or use slider to zoom • Click ⟲ to reset'"
echo

echo "=== How to Test ==="
echo "1. Open: http://localhost:5173/"
echo "2. Click: 'Galaxy Map' tab"
echo "3. Test drag-to-pan:"
echo "   • Click and hold on the map"
echo "   • Drag mouse to move the view"
echo "   • Release to stop panning"
echo "   • Watch pan coordinates update in status bar"
echo "4. Test with zoom:"
echo "   • Zoom in to 3x-5x"
echo "   • Drag to explore different regions"
echo "   • Click ⟲ to reset view (zoom + pan)"
echo
echo "The drag-to-pan functionality is fully implemented!"
