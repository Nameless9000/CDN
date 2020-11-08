import { Request, Response, Router } from 'express';
import { s3 } from '../utils/S3Util';
import Files, { File } from '../models/FileModel';
import InvisibleUrls from '../models/InvisibleUrlModel';
const router = Router();

router.get('/:id', async (req: Request, res: Response) => {
    const { id } = req.params;

    if (!id) return res.status(400).json({
        success: false,
        error: 'Provide a file id.',
    });

    let file: File;
    const invisibleUrl = await InvisibleUrls.findOne({ _id: id });

    invisibleUrl ?
        file = await Files.findOne({ filename: invisibleUrl.filename }) :
        file = await Files.findOne({ filename: id });

    if (!file) return res.status(404).json({
        success: false,
        error: 'invalid file',
    });

    const params = {
        Bucket: process.env.S3_BUCKET,
        Key: `${file.uploader.uid}/${file.filename}`,
    };

    try {
        const object = await s3.getObject(params).promise();
        const mimetype = file.mimetype.split('/')[0];

        if (mimetype === 'video') {
            res.set('Content-Type', object.ContentType);
            res.send(object.Body);
        } else if (mimetype === 'image') {
            if (file.displayType === 'embed') return res.render('embedFile', {
                url: `https://cdn.astral.cool/${file.uploader.uid}/${file.filename}`,
                uploader: file.uploader.username,
                date: file.dateUploaded,
                embed: {
                    title: file.embed.title === 'default' ? file.filename : file.embed.title,
                    description: file.embed.description === 'default' ? `Uploaded by ${file.uploader.username} on ${file.dateUploaded}.` : file.embed.description,
                    color: file.embed.color,
                },
            });

            if (file.showLink) return res.render('file', {
                url: `https://cdn.astral.cool/${file.uploader.uid}/${file.filename}`,
                uploader: file.uploader.username,
                date: file.dateUploaded,
            });

            res.set('Content-Type', object.ContentType);
            res.send(object.Body);
        }
    } catch (err) {
        res.status(500).json({
            success: false,
            error: err.message,
        });
    }
});

export default router;
