#!/usr/bin/env python3
"""
Extract grid elements from mining material screenshots - IMPROVED VERSION
Features:
- Configurable grid size (rows/cols)
- Visual debugging with grid overlay
- Better margin handling
"""

from PIL import Image, ImageDraw, ImageFont
import os
import sys

def create_debug_image(img, grid_start_x, grid_start_y, cell_width, cell_height, rows, cols, output_path):
    """Create a debug image showing grid lines"""
    debug = img.copy()
    draw = ImageDraw.Draw(debug)

    # Draw grid lines
    for row in range(rows + 1):
        y = grid_start_y + (row * cell_height)
        draw.line([(grid_start_x, y), (grid_start_x + (cols * cell_width), y)], fill='red', width=2)

    for col in range(cols + 1):
        x = grid_start_x + (col * cell_width)
        draw.line([(x, grid_start_y), (x, grid_start_y + (rows * cell_height))], fill='red', width=2)

    debug.save(output_path)
    print(f"Debug image saved to: {output_path}")

def extract_grid_with_debug(
    image_path,
    output_dir,
    grid_start_x=None,
    grid_start_y=None,
    grid_end_x=None,
    grid_end_y=None,
    rows=8,
    cols=5,
    cell_margin=4,
    debug=True,
    page_num=1
):
    """
    Extract grid elements with precise control.

    Parameters can be specified as pixels or percentages (as float 0-1).
    If not specified, uses intelligent defaults.
    """
    img = Image.open(image_path)
    img_width, img_height = img.size

    print(f"Image size: {img_width}x{img_height}")

    # Convert percentage coordinates to pixels
    def to_pixels(val, max_val, default):
        if val is None:
            return default
        if isinstance(val, float) and 0 <= val <= 1:
            return int(val * max_val)
        return val

    # Default grid boundaries (adjustable)
    grid_start_x = to_pixels(grid_start_x, img_width, int(img_width * 0.05))
    grid_start_y = to_pixels(grid_start_y, img_height, int(img_height * 0.12))
    grid_end_x = to_pixels(grid_end_x, img_width, int(img_width * 0.98))
    grid_end_y = to_pixels(grid_end_y, img_height, int(img_height * 0.95))

    # Calculate grid dimensions
    grid_width = grid_end_x - grid_start_x
    grid_height = grid_end_y - grid_start_y

    cell_width = grid_width // cols
    cell_height = grid_height // rows

    print(f"Grid area: ({grid_start_x}, {grid_start_y}) to ({grid_end_x}, {grid_end_y})")
    print(f"Grid size: {grid_width}x{grid_height}")
    print(f"Cell size: {cell_width}x{cell_height}")
    print(f"Grid: {rows} rows x {cols} cols = {rows * cols} cells")

    # Create debug image
    if debug:
        debug_path = os.path.join(output_dir, f"page{page_num:02d}_debug.png")
        create_debug_image(img, grid_start_x, grid_start_y, cell_width, cell_height, rows, cols, debug_path)

    # Extract each cell
    for row in range(rows):
        for col in range(cols):
            left = grid_start_x + (col * cell_width)
            top = grid_start_y + (row * cell_height)
            right = left + cell_width
            bottom = top + cell_height

            # Apply margin within cell
            crop_box = (
                left + cell_margin,
                top + cell_margin,
                right - cell_margin,
                bottom - cell_margin
            )

            cell = img.crop(crop_box)
            filename = f"page{page_num:02d}_r{row+1:02d}_c{col+1:02d}.png"
            output_path = os.path.join(output_dir, filename)
            cell.save(output_path)

    print(f"\nExtracted {rows * cols} cells to {output_dir}")
    return rows * cols

if __name__ == "__main__":
    output_dir = "/home/robert/spacemolt/spacemolt/temp"

    # Based on the image, appears to be approximately:
    # - 5 rows of items
    # - 7 columns of items
    # Grid starts around y=67px (12%), ends around y=536px (96%)
    # Grid starts around x=50px (5%), ends around x=1003px (98%)

    if len(sys.argv) > 1:
        input_file = sys.argv[1]
    else:
        input_file = "/home/robert/Downloads/ore-2.jpg"

    extract_grid_with_debug(
        input_file,
        output_dir,
        rows=5,
        cols=7,
        grid_start_y=0.12,    # Start at 12% down
        grid_end_y=0.96,      # End at 96% down
        grid_start_x=0.05,    # Start at 5% from left
        grid_end_x=0.98,      # End at 98% from right
        cell_margin=3,
        debug=True
    )
