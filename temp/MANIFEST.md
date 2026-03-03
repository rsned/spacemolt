# Mining Materials Grid Extract - Page 1

Extracted from: `/home/robert/Downloads/ore-2.jpg`

| File | Material Name | Quantity |
|------|---------------|----------|
| page01_row01_col01.png | Roussosite | x1239 |
| page01_row01_col02.png | Vesarite | x541 |
| page01_row01_col03.png | Fasroid (晶G?) | - |
| page01_row01_col04.png | - | - |
| page01_row02_col01.png | - | - |
| page01_row02_col02.png | - | - |
| page01_row02_col03.png | - | - |
| page01_row02_col04.png | - | - |
| page01_row03_col01.png | - | - |
| page01_row03_col02.png | - | - |
| page01_row03_col03.png | - | - |
| page01_row03_col04.png | - | - |
| page01_row04_col01.png | - | - |
| page01_row04_col02.png | - | - |
| page01_row04_col03.png | - | - |
| page01_row04_col04.png | - | - |

## Notes
- Grid layout: 4 rows × 4 columns
- Naming convention: `page{XX}_row{YY}_col{ZZ}.png`
- For multiple screens, increment the page number

## Usage
To process more images:
```bash
# Single image
python3 temp/extract_grid.py /path/to/your/screenshot.jpg

# Batch process (wildcard)
python3 temp/extract_grid.py "/path/to/ore-*.jpg"
```
