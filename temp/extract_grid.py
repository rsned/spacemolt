#!/usr/bin/env python3
"""
Extract grid elements from mining material screenshots.
Saves each cell as an individual image with systematic naming.
"""

from PIL import Image
import os
import sys
import re

def extract_grid(image_path, output_dir, rows=4, cols=4, page_num=1):
    """
    Extract grid elements from a screenshot.

    Args:
        image_path: Path to source image
        output_dir: Directory to save extracted images
        rows: Number of rows in grid (default 4)
        cols: Number of columns in grid (default 4)
        page_num: Page number for naming (default 1)
    """
    # Open the image
    img = Image.open(image_path)
    width, height = img.size

    print(f"Image size: {width}x{height}")

    # Calculate grid cell size
    # From the image, we need to identify the grid boundaries
    # The grid appears to have some padding around it

    # Estimate grid area (you may need to adjust these based on actual image)
    padding_x = int(width * 0.08)  # 8% padding on sides
    padding_top = int(height * 0.15)  # 15% padding on top
    padding_bottom = int(height * 0.05)  # 5% padding on bottom

    grid_width = width - (2 * padding_x)
    grid_height = height - padding_top - padding_bottom

    cell_width = grid_width // cols
    cell_height = grid_height // rows

    print(f"Grid area: {grid_width}x{grid_height}")
    print(f"Cell size: {cell_width}x{cell_height}")

    # Extract each cell
    for row in range(rows):
        for col in range(cols):
            # Calculate crop coordinates
            left = padding_x + (col * cell_width)
            top = padding_top + (row * cell_height)
            right = left + cell_width
            bottom = top + cell_height

            # Add small margin within cell for better visual separation
            margin = 5
            crop_box = (
                max(0, left + margin),
                max(0, top + margin),
                min(width, right - margin),
                min(height, bottom - margin)
            )

            # Crop and save
            cell = img.crop(crop_box)
            filename = f"page{page_num:02d}_row{row+1:02d}_col{col+1:02d}.png"
            output_path = os.path.join(output_dir, filename)
            cell.save(output_path)
            print(f"Saved: {filename}")

    print(f"\nExtracted {rows * cols} cells to {output_dir}")

def batch_extract(input_pattern, output_dir):
    """
    Batch extract from multiple images matching a pattern.
    """
    # Get list of matching files
    import glob
    files = sorted(glob.glob(input_pattern))

    if not files:
        print(f"No files found matching: {input_pattern}")
        return

    print(f"Found {len(files)} files to process")

    for page_num, filepath in enumerate(files, start=1):
        print(f"\n{'='*60}")
        print(f"Processing: {filepath}")
        print(f"{'='*60}")
        extract_grid(filepath, output_dir, page_num=page_num)

if __name__ == "__main__":
    output_dir = "/home/robert/spacemolt/spacemolt/temp"

    # Default: process the single image provided
    if len(sys.argv) > 1:
        input_files = sys.argv[1]
    else:
        input_files = "/home/robert/Downloads/ore-2.jpg"

    if "*" in input_files:
        batch_extract(input_files, output_dir)
    else:
        extract_grid(input_files, output_dir)
